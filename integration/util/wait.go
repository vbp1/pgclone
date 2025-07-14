//go:build integration
// +build integration

package util

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// WaitPostgresReady polls pg_isready inside container until it returns 0.
func WaitPostgresReady(ctx context.Context, container string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		ready := exec.CommandContext(ctx, "docker", "exec", container, "pg_isready", "-U", "postgres")
		if err := ready.Run(); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not become ready", container)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
