package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// WaitReplicationStarted waits until an application_name appears in pg_stat_replication or timeout.
func WaitReplicationStarted(ctx context.Context, q queryer, appName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var exists bool
		err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_replication WHERE application_name=$1)`, appName).Scan(&exists)
		if err != nil {
			return fmt.Errorf("query pg_stat_replication: %w", err)
		}
		if exists {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("replication did not start within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}
