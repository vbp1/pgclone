package rsync

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)


// RunParallel starts N rsync workers to transfer provided files to dstDir.
// It blocks until all workers finish or ctx is canceled.
// Returned error – first non-zero exit or context cancellation.
func RunParallel(ctx context.Context, cfg Config, module string, workers int, files []FileInfo, dstDir string, showBar bool, progressMode string, progressInterval int) (Stats, error) {
	start := time.Now()
	
	if workers <= 0 {
		workers = max(runtime.NumCPU()/2, 1)
	}

	const flushInterval = 500 * time.Millisecond
	// Split files among workers
	buckets := Distribute(files, workers)

	// --- calculate precise amount of bytes to transfer (dry-run) ---
	var totalBytes int64
	{
		// create temporary list with all files
		allList, err := os.CreateTemp("", "pgclone_all_files_*.txt")
		if err == nil {
			_ = allList.Close()
			if err := writeFiles(allList.Name(), files); err == nil {
				// build dry-run command
				dryCmd := cfg.BuildCmd(ctx, module, allList.Name(), dstDir)
				// prepend flags: --dry-run and use numeric %l output only
				dryCmd.Args = append([]string{dryCmd.Args[0]}, append([]string{"--dry-run", "--out-format=%l"}, dryCmd.Args[1:]...)...)
				out, err := dryCmd.Output()
				if err == nil {
					scanner := bufio.NewScanner(bytes.NewReader(out))
					for scanner.Scan() {
						if n, err := parseSize(scanner.Text()); err == nil && n > 0 {
							totalBytes += n
						}
					}
				}
			}
			_ = os.Remove(allList.Name())
		}
	}
	// fallback to naive sum if dry-run returned 0
	if totalBytes == 0 {
		for _, f := range files {
			totalBytes += f.Size
		}
	}

	// Log which module we are about to sync – printed before progress bar appears
	slog.Info("syncing module", "module", module)

	// prepare progress display
	var p *mpb.Progress
	var bar *mpb.Bar
	var showPlain bool

	// === Shared progress state (for plain mode) ===
	var progressBytes int64
	var progressMu sync.Mutex

	if showBar {
		p = mpb.New(mpb.WithWidth(40), mpb.WithRefreshRate(100*time.Millisecond))
		// Module name followed by space, then percentage
		namePrefix := module + " "
		bar = p.New(totalBytes, mpb.BarStyle().Rbound("|").Lbound("|"),
			mpb.PrependDecorators(decor.Name(namePrefix, decor.WC{W: len(namePrefix), C: decor.DSyncWidth}), decor.Percentage()),
			mpb.AppendDecorators(decor.Any(func(s decor.Statistics) string {
				return fmt.Sprintf("%s / %s", formatBytes(s.Current), formatBytes(s.Total))
			})))
	} else if progressMode == "plain" {
		showPlain = true
		if progressInterval <= 0 {
			progressInterval = 30
		}
	}

	tmpDir, err := os.MkdirTemp("", "pgclone_files")
	if err != nil {
		return Stats{}, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create shared pipes for progress and stats
	progressReader, progressWriter := io.Pipe()
	statsReader, statsWriter := io.Pipe()

	// WaitGroup for workers and goroutines
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	statsCh := make(chan Stats, workers)

	// Channel to signal when all workers are done writing
	workersFinished := make(chan struct{})

	// Start plain progress printer as separate goroutine if needed
	var plainDone chan struct{}
	if showPlain {
		plainDone = make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(time.Duration(progressInterval) * time.Second)
			defer ticker.Stop()
			startTime := time.Now()
			for {
				select {
				case <-ctx.Done():
					return
				case <-plainDone:
					return
				case <-ticker.C:
					progressMu.Lock()
					current := progressBytes
					progressMu.Unlock()

					elapsed := time.Since(startTime)
					percent := int64(0)
					if totalBytes > 0 {
						percent = min((current*100)/totalBytes, 100)
					}

					speed := int64(0)
					if elapsed.Seconds() > 0 {
						speed = int64(float64(current) / elapsed.Seconds())
					}

					remaining := totalBytes - current
					eta := int64(0)
					if speed > 0 {
						eta = remaining / speed
					}

					fmt.Fprintf(os.Stderr, "[%s] %3d %%  (%s / %s, %s/s, ETA %02d:%02d:%02d)\n",
						time.Now().Format("2006-01-02 15:04:05"),
						percent,
						formatBytes(current),
						formatBytes(totalBytes),
						formatBytes(speed),
						eta/3600,
						(eta%3600)/60,
						eta%60)

					// exit when done
					if current >= totalBytes {
						return
					}
				}
			}
		}()
	}

	// Start consolidated progress tracking goroutine  
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer progressReader.Close()
		
		br := bufio.NewReaderSize(progressReader, 256*1024)
		pending := 0
		lastFlush := time.Now()
		lineCount := 0
		
		for {
			line, err := br.ReadBytes('\n')
			if len(line) > 0 {
				lineCount++
				slog.Debug("rsync stdout", "line_num", lineCount, "line", string(line))
				
				if bar != nil || showPlain {
					if n, ok := parseSizeBytes(line); ok && n > 0 {
						if bar != nil {
							pending += int(n)
						}
						if showPlain {
							progressMu.Lock()
							progressBytes += n
							progressMu.Unlock()
						}
					}
				}
			}
			flush := false
			if pending > 0 && (time.Since(lastFlush) > flushInterval || err == io.EOF) {
				flush = true
			}
			if flush && bar != nil {
				bar.IncrBy(pending)
				pending = 0
				lastFlush = time.Now()
			}
			if err != nil {
				if err == io.EOF {
					break
				}
				break
			}
		}
		
		slog.Debug("rsync stdout complete", "total_lines", lineCount)
		
		if showPlain && plainDone != nil {
			close(plainDone)
		}
	}()

	// Start consolidated stderr/logging goroutine - just for logging, no stats parsing
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer statsReader.Close()
		
		lineCount := 0
		sc := bufio.NewScanner(statsReader)
		for sc.Scan() {
			line := sc.Text()
			lineCount++
			slog.Debug("rsync stderr", "line_num", lineCount, "line", line)
		}
		
		slog.Debug("rsync stderr complete", "total_lines", lineCount)
	}()

	// Separate WaitGroup for workers only
	var workersWG sync.WaitGroup

	// Launch workers
	for idx, bucket := range buckets {
		if len(bucket) == 0 {
			continue
		}
		listPath := filepath.Join(tmpDir, fmt.Sprintf("files_%d.txt", idx))
		if err := writeFiles(listPath, bucket); err != nil {
			return Stats{}, err
		}

		// Calculate worker statistics for debugging
		var workerTotalSize int64
		var workerLargestFile int64
		var workerSmallestFile int64 = files[0].Size // Initialize with first file size
		
		for _, f := range bucket {
			workerTotalSize += f.Size
			if f.Size > workerLargestFile {
				workerLargestFile = f.Size
			}
			if f.Size < workerSmallestFile {
				workerSmallestFile = f.Size
			}
		}
		
		slog.Info("worker starting", 
			"worker_id", idx,
			"module", module,
			"file_count", len(bucket),
			"total_size_gb", float64(workerTotalSize)/(1024*1024*1024),
			"largest_file_gb", float64(workerLargestFile)/(1024*1024*1024),
			"smallest_file_mb", float64(workerSmallestFile)/(1024*1024),
			"avg_file_mb", float64(workerTotalSize/int64(len(bucket)))/(1024*1024))

		// Build rsync command
		rsyncCmd := cfg.BuildCmd(ctx, module, listPath, dstDir)
		rsyncCmd.Args = append([]string{rsyncCmd.Args[0]}, append([]string{"--out-format=%l"}, rsyncCmd.Args[1:]...)...)

		// Create awk pass-through commands for stdout and stderr with flush
		awkStdout := exec.CommandContext(ctx, "awk", "{print; fflush()}")
		awkStderr := exec.CommandContext(ctx, "awk", "{print; fflush()}")

		// Connect rsync outputs to awk inputs
		awkStdout.Stdin, _ = rsyncCmd.StdoutPipe()
		awkStderr.Stdin, _ = rsyncCmd.StderrPipe()

		// create per-worker log file
		logPath := filepath.Join(tmpDir, fmt.Sprintf("worker_%d.log", idx))
		logFile, _ := os.Create(logPath)

		// Use shared pipes directly - no need for workerWriter wrapper
		progressWorkerWriter := progressWriter
		statsWorkerWriter := statsWriter

		// Connect awk outputs to shared pipes
		if logFile != nil {
			awkStdout.Stdout = io.MultiWriter(progressWorkerWriter, logFile)
			awkStderr.Stdout = io.MultiWriter(statsWorkerWriter, logFile)
		} else {
			awkStdout.Stdout = progressWorkerWriter
			awkStderr.Stdout = statsWorkerWriter
		}

		// Store awk commands for proper lifecycle management
		var awkCommands []*exec.Cmd
		awkCommands = append(awkCommands, awkStdout, awkStderr)

		// Start rsync command first
		if err := rsyncCmd.Start(); err != nil {
			return Stats{}, err
		}

		// Start awk commands with proper error handling
		var startedAwkCommands []*exec.Cmd
		for i, awkCmd := range awkCommands {
			if err := awkCmd.Start(); err != nil {
				// Cleanup: kill rsync and any already started awk commands
				rsyncCmd.Process.Kill()
				for _, started := range startedAwkCommands {
					started.Process.Kill()
				}
				return Stats{}, fmt.Errorf("failed to start awk command %d: %w", i, err)
			}
			startedAwkCommands = append(startedAwkCommands, awkCmd)
		}

		workersWG.Add(1)
		go func(rsync *exec.Cmd, awks []*exec.Cmd, widx int, lf *os.File, workerStats map[string]interface{}) {
			defer workersWG.Done()
			
			startTime := time.Now()
			
			// Context-aware cleanup function - close file only on context cancel
			cleanup := func() {
				// File will be closed and read after successful completion
			}
			defer cleanup()
			
			// Channel to handle context cancellation
			done := make(chan struct{})
			var rsyncErr error
			var awkErrors []error
			var rsyncStartTime time.Time
			var rsyncEndTime time.Time
			
			go func() {
				defer close(done)
				
				// Wait for rsync to finish first
				rsyncStartTime = time.Now()
				rsyncErr = rsync.Wait()
				rsyncEndTime = time.Now()
				
				// Wait for awk commands to finish processing remaining data
				for i, awkCmd := range awks {
					if err := awkCmd.Wait(); err != nil {
						slog.Debug("awk command failed", "worker", widx, "awk_idx", i, "error", err)
						awkErrors = append(awkErrors, err)
					}
				}
			}()
			
			select {
			case <-ctx.Done():
				// Context cancelled - force kill all processes
				totalTime := time.Since(startTime)
				slog.Warn("worker cancelled", 
					"worker_id", widx,
					"module", module,
					"status", "context_cancelled",
					"total_time_sec", totalTime.Seconds(),
					"initial_file_count", workerStats["file_count"],
					"initial_total_size_gb", workerStats["total_size_gb"])
				
				rsync.Process.Kill()
				for _, awkCmd := range awks {
					if awkCmd.Process != nil {
						awkCmd.Process.Kill()
					}
				}
				// Wait for cleanup to complete
				<-done
				// Close log file on context cancel
				if lf != nil {
					lf.Close()
				}
				errCh <- ctx.Err()
				return
			case <-done:
				// Normal completion
			}
			
			// Report error if rsync failed (priority over awk errors)
			if rsyncErr != nil {
				totalTime := time.Since(startTime)
				rsyncTime := rsyncEndTime.Sub(rsyncStartTime)
				
				slog.Error("worker failed", 
					"worker_id", widx,
					"module", module,
					"status", "rsync_error",
					"error", rsyncErr.Error(),
					"total_time_sec", totalTime.Seconds(),
					"rsync_time_sec", rsyncTime.Seconds(),
					"initial_file_count", workerStats["file_count"],
					"initial_total_size_gb", workerStats["total_size_gb"])
				
				errCh <- rsyncErr
				return
			}
			
			// Report awk errors only if rsync succeeded
			if len(awkErrors) > 0 {
				totalTime := time.Since(startTime)
				rsyncTime := rsyncEndTime.Sub(rsyncStartTime)
				
				slog.Error("worker failed", 
					"worker_id", widx,
					"module", module,
					"status", "awk_error",
					"error", fmt.Sprintf("awk errors: %v", awkErrors),
					"total_time_sec", totalTime.Seconds(),
					"rsync_time_sec", rsyncTime.Seconds(),
					"initial_file_count", workerStats["file_count"],
					"initial_total_size_gb", workerStats["total_size_gb"])
				
				errCh <- fmt.Errorf("worker %d awk errors: %v", widx, awkErrors)
				return
			}

			// Parse worker's stats from log file if rsync succeeded
			if lf != nil {
				lf.Close() // Close for reading
				if content, err := os.ReadFile(lf.Name()); err == nil {
					if st, err := ParseStats(bufio.NewScanner(bytes.NewReader(content))); err == nil {
						totalTime := time.Since(startTime)
						rsyncTime := rsyncEndTime.Sub(rsyncStartTime)
						
						// Calculate transfer rate
						var transferRate float64
						if totalTime.Seconds() > 0 {
							transferRate = float64(st.BytesReceived) / (1024 * 1024) / totalTime.Seconds()
						}
						
						slog.Info("worker completed", 
							"worker_id", widx,
							"module", module,
							"status", "success",
							"total_time_sec", totalTime.Seconds(),
							"rsync_time_sec", rsyncTime.Seconds(),
							"setup_time_sec", (totalTime - rsyncTime).Seconds(),
							"files_processed", st.NumFiles,
							"files_transferred", st.RegTransferred,
							"bytes_received_gb", float64(st.BytesReceived)/(1024*1024*1024),
							"bytes_sent_mb", float64(st.BytesSent)/(1024*1024),
							"transfer_rate_mbps", transferRate,
							"literal_data_gb", float64(st.LiteralData)/(1024*1024*1024),
							"matched_data_gb", float64(st.MatchedData)/(1024*1024*1024),
							"initial_file_count", workerStats["file_count"],
							"initial_total_size_gb", workerStats["total_size_gb"],
							"initial_largest_file_gb", workerStats["largest_file_gb"],
							"initial_smallest_file_mb", workerStats["smallest_file_mb"],
							"initial_avg_file_mb", workerStats["avg_file_mb"])
						
						select {
						case statsCh <- st:
						case <-ctx.Done():
							// Don't block if context is cancelled
						}
					} else {
						slog.Error("worker stats parse error", "worker", widx, "error", err)
					}
				} else {
					slog.Error("worker log file read error", "worker", widx, "error", err)
				}
			}
		}(rsyncCmd, startedAwkCommands, idx, logFile, map[string]interface{}{
			"file_count": len(bucket),
			"total_size_gb": float64(workerTotalSize)/(1024*1024*1024),
			"largest_file_gb": float64(workerLargestFile)/(1024*1024*1024),
			"smallest_file_mb": float64(workerSmallestFile)/(1024*1024),
			"avg_file_mb": float64(workerTotalSize/int64(len(bucket)))/(1024*1024),
		})
	}

	// Start goroutine to close shared pipes when all workers are done
	go func() {
		// Wait for all worker goroutines to finish
		workersWG.Wait()
		
		// Close write ends of pipes to signal readers to finish
		progressWriter.Close()
		statsWriter.Close()
		
		// Now wait for readers to finish and signal completion
		wg.Wait()
		close(workersFinished)
	}()

	var total Stats

	select {
	case <-ctx.Done():
		return total, ctx.Err()
	case err := <-errCh:
		return total, err
	case <-workersFinished:
		// Complete the bar to exactly 100%
		if bar != nil && p != nil {
			// Calculate remaining bytes to reach 100%
			current := bar.Current()
			if remaining := totalBytes - current; remaining > 0 {
				bar.IncrInt64(remaining)
			}
			bar.SetTotal(totalBytes, true) // mark as complete
			p.Wait()
		}
		
		// Close statsCh and collect remaining stats with timeout protection
		close(statsCh)
		timeout := time.After(1 * time.Second)
	statsLoop:
		for {
			select {
			case st, ok := <-statsCh:
				if !ok {
					// Channel closed, no more stats
					break statsLoop
				}
				total = total.Add(st)
			case <-timeout:
				// Timeout protection - don't wait forever for stats
				break statsLoop
			}
		}
		
		// Use precise progress counter for BytesReceived instead of aggregated per-worker stats
		// This prevents double counting and multiplication errors similar to bash implementation
		progressMu.Lock()
		accurateProgressBytes := progressBytes
		progressMu.Unlock()
		
		if accurateProgressBytes > 0 {
			// Override BytesReceived with the accurate progress counter
			total.BytesReceived = accurateProgressBytes
		}
		
		// Log summary statistics for all workers
		slog.Info("all workers completed", 
			"module", module,
			"total_workers", workers,
			"total_bytes_received_gb", float64(total.BytesReceived)/(1024*1024*1024),
			"total_files_processed", total.NumFiles,
			"total_files_transferred", total.RegTransferred,
			"total_time_sec", time.Since(start).Seconds())
		
		return total, nil
	}
}

func writeFiles(path string, files []FileInfo) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	for _, fi := range files {
		if _, err := fmt.Fprintln(f, fi.Path); err != nil {
			return err
		}
	}
	return nil
}

func parseSize(line string) (int64, error) {
	var n int64
	_, err := fmt.Sscanf(line, "%d", &n)
	return n, err
}

// parseSizeBytes parses leading decimal digits from a byte slice and returns the integer value.
// It avoids allocations by not converting the slice to string.
func parseSizeBytes(b []byte) (int64, bool) {
	var n int64
	parsed := false
	for _, c := range b {
		if c >= '0' && c <= '9' {
			n = n*10 + int64(c-'0')
			parsed = true
		} else {
			// stop at first non-digit
			break
		}
	}
	return n, parsed
}
