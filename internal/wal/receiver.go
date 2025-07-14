package wal

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// Receiver wraps pg_receivewal process lifecycle.
type Receiver struct {
	Host    string
	Port    int
	User    string
	Dir     string // target directory for WAL
	Slot    string // optional; empty = no slot
	Verbose bool

	AppName string // optional application_name (sets PGAPPNAME)
	cmd     *exec.Cmd
	wg      sync.WaitGroup
	mu      sync.Mutex
	closed  bool
}

// Start launches pg_receivewal in background.
func (r *Receiver) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil {
		return fmt.Errorf("pg_receivewal already started")
	}
	if r.Dir == "" {
		return fmt.Errorf("dir not specified")
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return err
	}

	args := []string{
		"--host", r.Host,
		"--port", fmt.Sprintf("%d", r.Port),
		"--username", r.User,
		"--no-password",
		"--directory", r.Dir,
	}
	if r.Slot != "" {
		args = append(args, "--slot", r.Slot)
	}
	if r.Verbose {
		args = append(args, "--verbose")
	}

	bin, err := exec.LookPath("pg_receivewal")
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	if r.AppName != "" {
		// inherit current env and add PGAPPNAME
		cmd.Env = append(os.Environ(), "PGAPPNAME="+r.AppName)
	}
	// Redirect outputs to log file under Dir
	logFile := filepath.Join(r.Dir, "pg_receivewal.log")
	lf, err := os.Create(logFile)
	if err != nil {
		return err
	}
	cmd.Stdout = lf
	cmd.Stderr = lf

	if err := cmd.Start(); err != nil {
		_ = lf.Close()
		return err
	}

	r.cmd = cmd
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		err := cmd.Wait()
		_ = lf.Close()
		if err != nil && !r.closed {
			slog.Warn("pg_receivewal exited", "err", err)
		}
	}()

	return nil
}

// Stop terminates pg_receivewal process gracefully.
func (r *Receiver) Stop() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	cmd := r.cmd
	r.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// Send SIGTERM
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		// after done, drop replication slot if set
		if r.Slot != "" {
			// drop slot via pg_receivewal --drop-slot
			dropCmd := exec.Command("pg_receivewal",
				"--host", r.Host,
				"--port", fmt.Sprintf("%d", r.Port),
				"--username", r.User,
				"--no-password", "--drop-slot", "--slot", r.Slot)
			_ = dropCmd.Run()
		}
		return nil
	case <-context.Background().Done():
		return fmt.Errorf("context closed")
	}
}
