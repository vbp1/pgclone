//go:build integration
// +build integration

package integration

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vbp1/pgclone/integration/util"
)

func TestHappyPath(t *testing.T) {
	require := require.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	composeFile := filepath.Join("compose.yml")
	project := "pgclone"
	teardown, err := util.StartCompose(ctx, composeFile, project)
	require.NoError(err)
	defer teardown()

	// wait for primary ready (container name has compose prefix)
	primaryContainer := fmt.Sprintf("%s-pg-primary-1", project)
	require.NoError(util.WaitPostgresReady(ctx, primaryContainer, 1*time.Minute))

	// run pgclone inside replica
	replicaContainer := fmt.Sprintf("%s-pg-replica-1", project)
	cmd := exec.CommandContext(ctx, "docker", "exec", "-u", "postgres", "-e", "PGPASSWORD=postgres", replicaContainer,
		"pgclone", "--pghost", "pg-primary", "--pguser", "postgres", "--primary-pgdata", "/var/lib/postgresql/data",
		"--replica-pgdata", "/var/lib/postgresql/data", "--ssh-user", "postgres", "--ssh-key", "/var/lib/postgresql/.ssh/id_rsa", "--insecure-ssh", "--slot", "--verbose")
	out, err := cmd.CombinedOutput()
	require.NoErrorf(err, "pgclone failed: %s", string(out))

	// verify PG_VERSION exists
	cat := exec.CommandContext(ctx, "docker", "exec", replicaContainer, "cat", "/var/lib/postgresql/data/PG_VERSION")
	pgv, err := cat.Output()
	require.NoError(err)
	require.Contains(string(pgv), "15")
}
