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
	"golang.org/x/sys/unix"
)

// RunParallel starts N rsync workers to transfer provided files to dstDir.
// It blocks until all workers finish or ctx is canceled.
// Returned error – first non-zero exit or context cancellation.
func RunParallel(ctx context.Context, cfg Config, module string, workers int, files []FileInfo, dstDir string, showBar bool, progressMode string, progressInterval int) (Stats, error) {
	if workers <= 0 {
		workers = runtime.NumCPU() / 2
		if workers == 0 {
			workers = 1
		}
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

	// WaitGroup for workers
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	statsCh := make(chan Stats, workers)

	// Start single plain progress printer goroutine (once)
	if showPlain {
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
				case <-ticker.C:
					progressMu.Lock()
					current := progressBytes
					progressMu.Unlock()

					elapsed := time.Since(startTime)
					percent := int64(0)
					if totalBytes > 0 {
						percent = (current * 100) / totalBytes
						if percent > 100 {
							percent = 100
						}
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

	// Launch workers
	for idx, bucket := range buckets {
		if len(bucket) == 0 {
			continue
		}
		listPath := filepath.Join(tmpDir, fmt.Sprintf("files_%d.txt", idx))
		if err := writeFiles(listPath, bucket); err != nil {
			return Stats{}, err
		}
		// ### DEBUG CODE: worker start log
		// slog.Info("rsync worker start", "idx", idx, "files", len(bucket), "list", listPath)

		// we assume same module for all (base), caller may call per module separately
		// module := "base" // This line is removed as per the new_code
		cmd := cfg.BuildCmd(ctx, module, listPath, dstDir)
		// ensure %l output format to get transferred size per file
		cmd.Args = append([]string{cmd.Args[0]}, append([]string{"--out-format=%l"}, cmd.Args[1:]...)...)

		// create per-worker log file
		logPath := filepath.Join(tmpDir, fmt.Sprintf("worker_%d.log", idx))
		logFile, _ := os.Create(logPath) // ошибки игнорируем – не критично

		stdout, _ := cmd.StdoutPipe()
		errPipe, _ := cmd.StderrPipe()

		// enlarge pipe buffer to 1 MiB to reduce blocking
		if f, ok := stdout.(*os.File); ok {
			_, _ = unix.FcntlInt(f.Fd(), unix.F_SETPIPE_SZ, 1<<20)
		}
		if f, ok := errPipe.(*os.File); ok {
			_, _ = unix.FcntlInt(f.Fd(), unix.F_SETPIPE_SZ, 1<<20)
		}
		var stderr io.Reader = errPipe
		if logFile != nil {
			stderr = io.TeeReader(errPipe, logFile)
		}
		var statsBuf bytes.Buffer
		var statsMu sync.Mutex

		if err := cmd.Start(); err != nil {
			return Stats{}, err
		}

		// Progress tracking goroutine
		wg.Add(1)
		go func(r io.Reader) {
			defer wg.Done()
			br := bufio.NewReaderSize(r, 256*1024)
			pending := 0
			lastFlush := time.Now()
			for {
				line, err := br.ReadBytes('\n')
				if len(line) > 0 {
					statsMu.Lock()
					statsBuf.Write(line)
					statsMu.Unlock()
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
			if logFile != nil {
				_ = logFile.Close()
			}
		}(stdout)

		// Plain progress printer goroutine will be started once after launching all workers

		// read stderr, log and collect for stats
		wg.Add(1)
		go func(r io.Reader) {
			defer wg.Done()
			sc := bufio.NewScanner(r)
			for sc.Scan() {
				line := sc.Text()
				slog.Debug("rsync", "line", line)
				statsMu.Lock()
				statsBuf.WriteString(line)
				statsBuf.WriteByte('\n')
				statsMu.Unlock()
			}
			// parse stats after reading complete
			if st, err := ParseStats(bufio.NewScanner(bytes.NewReader(statsBuf.Bytes()))); err == nil {
				statsCh <- st
			}
		}(stderr)

		wg.Add(1)
		go func(c *exec.Cmd, widx int) {
			defer wg.Done()
			if err := c.Wait(); err != nil {
				// ### DEBUG CODE: worker finished with error
				// slog.Info("rsync worker done with error", "idx", widx, "err", err)
				errCh <- err
			}
			// ### DEBUG CODE: worker finished successfully
			// slog.Info("rsync worker done", "idx", widx, "err", nil)
		}(cmd, idx)
	}

	// Wait for all goroutines
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	var total Stats

	select {
	case <-ctx.Done():
		return total, ctx.Err()
	case err := <-errCh:
		return total, err
	case <-done:
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
		close(statsCh)
		for st := range statsCh {
			total = total.Add(st)
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
