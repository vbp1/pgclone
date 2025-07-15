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
	"sync/atomic"
	"time"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

// workerWriter wraps a writer with atomic close state
type workerWriter struct {
	writer io.Writer
	closed int64 // atomic boolean: 0 = open, 1 = closed
}

func (w *workerWriter) Write(p []byte) (n int, err error) {
	if atomic.LoadInt64(&w.closed) != 0 {
		return 0, io.ErrClosedPipe
	}
	return w.writer.Write(p)
}

func (w *workerWriter) Close() error {
	atomic.StoreInt64(&w.closed, 1)
	return nil
}

// RunParallel starts N rsync workers to transfer provided files to dstDir.
// It blocks until all workers finish or ctx is canceled.
// Returned error – first non-zero exit or context cancellation.
func RunParallel(ctx context.Context, cfg Config, module string, workers int, files []FileInfo, dstDir string, showBar bool, progressMode string, progressInterval int) (Stats, error) {
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
		
		for {
			line, err := br.ReadBytes('\n')
			if len(line) > 0 {
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
		
		if showPlain && plainDone != nil {
			close(plainDone)
		}
	}()

	// Start consolidated stderr/logging/stats goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer statsReader.Close()
		
		var statsBuf bytes.Buffer
		sc := bufio.NewScanner(statsReader)
		for sc.Scan() {
			line := sc.Text()
			slog.Debug("rsync", "line", line)
			statsBuf.WriteString(line)
			statsBuf.WriteByte('\n')
		}
		
		// Parse stats after reading complete
		if st, err := ParseStats(bufio.NewScanner(bytes.NewReader(statsBuf.Bytes()))); err == nil {
			select {
			case statsCh <- st:
			case <-ctx.Done():
				// Don't block if context is cancelled
			}
		}
	}()

	// Separate WaitGroup for workers only
	var workersWG sync.WaitGroup

	// Launch workers
	var workerWriters []*workerWriter
	for idx, bucket := range buckets {
		if len(bucket) == 0 {
			continue
		}
		listPath := filepath.Join(tmpDir, fmt.Sprintf("files_%d.txt", idx))
		if err := writeFiles(listPath, bucket); err != nil {
			return Stats{}, err
		}

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

		// Create worker-specific writers for shared pipes
		progressWorkerWriter := &workerWriter{writer: progressWriter}
		statsWorkerWriter := &workerWriter{writer: statsWriter}
		workerWriters = append(workerWriters, progressWorkerWriter, statsWorkerWriter)

		// Connect awk outputs to shared pipes
		if logFile != nil {
			awkStdout.Stdout = io.MultiWriter(progressWorkerWriter, logFile)
			awkStderr.Stderr = io.MultiWriter(statsWorkerWriter, logFile)
		} else {
			awkStdout.Stdout = progressWorkerWriter
			awkStderr.Stderr = statsWorkerWriter
		}

		// Store awk commands for proper lifecycle management
		var awkCommands []*exec.Cmd
		awkCommands = append(awkCommands, awkStdout, awkStderr)

		// Start rsync command first
		if err := rsyncCmd.Start(); err != nil {
			return Stats{}, err
		}

		// Start awk commands
		for _, awkCmd := range awkCommands {
			if err := awkCmd.Start(); err != nil {
				return Stats{}, err
			}
		}

		workersWG.Add(1)
		go func(rsync *exec.Cmd, awks []*exec.Cmd, widx int, lf *os.File) {
			defer workersWG.Done()
			
			// Wait for rsync to finish first
			rsyncErr := rsync.Wait()
			
			// Then wait for awk commands to finish processing remaining data
			for _, awkCmd := range awks {
				awkCmd.Wait()
			}
			
			// Report error if rsync failed
			if rsyncErr != nil {
				errCh <- rsyncErr
			}
			
			if lf != nil {
				_ = lf.Close()
			}
		}(rsyncCmd, awkCommands, idx, logFile)
	}

	// Start goroutine to close shared pipes when all workers are done
	go func() {
		// Wait for all worker goroutines to finish
		workersWG.Wait()
		
		// Close all worker writers first
		for _, w := range workerWriters {
			w.Close()
		}
		
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
