package process

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// KillChildrenOnCancel запускает горутину: при отмене ctx отправляет SIGTERM всем дочерним процессам текущего PID.
func KillChildrenOnCancel(ctx context.Context, grace time.Duration) {
	go func() {
		<-ctx.Done()
		pid := os.Getpid()
		slog.Warn("watchdog: context canceled, terminating children", "pid", pid)

		// Используем pgrep -P <pid>
		out, err := exec.Command("pgrep", "-P", strconv.Itoa(pid)).Output()
		if err != nil {
			slog.Warn("watchdog: pgrep", "err", err)
			return
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			childPID, _ := strconv.Atoi(line)
			slog.Info("watchdog: sending SIGTERM", "child", childPID)
			if err := syscall.Kill(childPID, syscall.SIGTERM); err != nil {
				slog.Warn("watchdog: SIGTERM failed", "pid", childPID, "err", err)
			}
		}
		time.Sleep(grace)
		// force kill remaining
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			childPID, _ := strconv.Atoi(line)
			if err := syscall.Kill(childPID, syscall.SIGKILL); err != nil {
				slog.Warn("watchdog: SIGKILL failed", "pid", childPID, "err", err)
			}
		}
	}()
}
