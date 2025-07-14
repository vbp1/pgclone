//go:build integration
// +build integration

package util

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

// StartCompose brings up docker-compose stack and returns a teardown func.
// composeFile is path to compose.yml, projectName becomes docker-compose -p <name>.
func StartCompose(ctx context.Context, composeFile, projectName string) (func() error, error) {
	absCompose, errAbs := filepath.Abs(composeFile)
	if errAbs != nil {
		return nil, fmt.Errorf("abs path: %w", errAbs)
	}

	up := exec.CommandContext(ctx, "docker", "compose", "-f", absCompose, "-p", projectName, "up", "-d", "--build")
	out, err := up.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker compose up: %w\n%s", err, string(out))
	}

	teardown := func() error {
		downCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		down := exec.CommandContext(downCtx, "docker", "compose", "-f", absCompose, "-p", projectName, "down", "-v")
		down.Stdout = nil
		down.Stderr = nil
		return down.Run()
	}
	return teardown, nil
}
