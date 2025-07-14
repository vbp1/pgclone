package rsync_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/vbp1/pgclone/internal/rsync"
)

func TestBuildCmd(t *testing.T) {
	cfg := rsync.Config{
		Host:       "127.0.0.1",
		Port:       45001,
		SecretFile: "/tmp/sec",
		Checksum:   true,
		Verbose:    false,
	}
	cmd := cfg.BuildCmd(context.Background(), "base", "/tmp/list", "/data/base")
	wantArgs := []string{
		"-a", "--relative", "--inplace", "--checksum",
		"--stats",
		"--exclude", "pgsql_tmp*",
		"--exclude", "pg_internal.init",
		"--files-from", "/tmp/list",
		"--password-file", "/tmp/sec",
		"rsync://replica@127.0.0.1:45001/base/",
		"/data/base/",
	}
	if !reflect.DeepEqual(cmd.Args[1:], wantArgs) { // Args[0] = rsync binary path
		t.Fatalf("args mismatch\nwant %v\n got %v", wantArgs, cmd.Args[1:])
	}
}
