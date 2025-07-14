package postgres

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect establishes a pgx pool. If dsn is empty, it is built from libpq-compatible
// environment variables (PGHOST, PGPORT, PGUSER, PGPASSWORD, PGDATABASE).
// maxConns=0 uses pgx default.
func Connect(ctx context.Context, dsn string, maxConns int32) (*pgxpool.Pool, error) {
	if dsn == "" {
		// minimal DSN
		host := os.Getenv("PGHOST")
		if host == "" {
			host = "localhost"
		}
		port := os.Getenv("PGPORT")
		if port == "" {
			port = "5432"
		}
		user := os.Getenv("PGUSER")
		if user == "" {
			user = os.Getenv("USER")
		}
		db := os.Getenv("PGDATABASE")
		if db == "" {
			db = "postgres"
		}
		dsn = fmt.Sprintf("postgres://%s@%s:%s/%s", user, host, port, db)
		// rely on PGPASSWORD env if present
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	cfg.MaxConnLifetime = time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// ping
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// EnsureVersion15Plus checks that server_version_num >= 150000.
func EnsureVersion15Plus(ctx context.Context, pool *pgxpool.Pool) error {
	var verStr string
	if err := pool.QueryRow(ctx, "SHOW server_version_num").Scan(&verStr); err != nil {
		return fmt.Errorf("query version: %w", err)
	}
	verNum, err := strconv.Atoi(verStr)
	if err != nil {
		return fmt.Errorf("parse version_num %s: %w", verStr, err)
	}
	if verNum < 150000 {
		return fmt.Errorf("PostgreSQL >= 15 required, server reports %s", verStr)
	}
	return nil
}

// Tablespace represents OID->location mapping.
type Tablespace struct {
	Oid      uint32
	Location string
}

// ListTablespaces returns OID/location for each user tablespace (excluding pg_default/global).
func ListTablespaces(ctx context.Context, pool *pgxpool.Pool) ([]Tablespace, error) {
	const q = `SELECT oid, pg_tablespace_location(oid)
              FROM pg_tablespace
              WHERE spcname NOT IN ('pg_default','pg_global')`
	rows, err := pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []Tablespace
	for rows.Next() {
		var t Tablespace
		if err := rows.Scan(&t.Oid, &t.Location); err != nil {
			return nil, err
		}
		res = append(res, t)
	}
	return res, rows.Err()
}

// PrettyBytes converts bytes to human-readable IEC units similar to pg_size_pretty.
func PrettyBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d bytes", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	value := float64(b) / float64(div)
	suffix := []string{"kB", "MB", "GB", "TB", "PB", "EB"}[exp]
	return fmt.Sprintf("%.2f %s", value, suffix)
}
