package process

import (
	"bytes"
	"context"
	"log/slog"
	"os/exec"
	"time"
)

// Result содержит данные о выполненной команде.
type Result struct {
	Cmd      string
	Args     []string
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Duration time.Duration
	Err      error
}

// RunLogged выполняет внешний процесс, логируя начало/конец и собирая вывод.
func RunLogged(ctx context.Context, bin string, args ...string) Result {
	cmd := exec.CommandContext(ctx, bin, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	slog.Info("exec start", "cmd", bin, "args", args)
	start := time.Now()

	err := cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	slog.Info("exec done", "cmd", bin, "code", exitCode, "dur", duration, "err", err)

	return Result{
		Cmd:      bin,
		Args:     args,
		Stdout:   outBuf.Bytes(),
		Stderr:   errBuf.Bytes(),
		ExitCode: exitCode,
		Duration: duration,
		Err:      err,
	}
}
