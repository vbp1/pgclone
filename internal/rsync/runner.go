package rsync

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
)

// Config holds parameters common for all rsync workers in a session.
type Config struct {
	Host       string // remote host
	Port       int    // rsync daemon port
	SecretFile string // local path to password file
	Checksum   bool   // use --checksum flag (paranoid)
	Verbose    bool   // add --stats --human-readable
}

// BuildCmd constructs *exec.Cmd to sync files listed in filesFrom into dstDir.
// filesFrom must be a plain text file with relative paths (one per line).
func (c Config) BuildCmd(ctx context.Context, module string, filesFrom string, dstDir string) *exec.Cmd {
	rsyncBin := "rsync"
	args := []string{"-a", "--relative", "--inplace"}
	if c.Checksum {
		args = append(args, "--checksum")
	}
	// Always enable stats for post-processing
	args = append(args, "--stats")
	if c.Verbose {
		args = append(args, "--human-readable")
	}
	// standard exclusions replicated from Bash version
	excludes := []string{"pgsql_tmp*", "pg_internal.init"}
	for _, e := range excludes {
		args = append(args, "--exclude", e)
	}

	args = append(args, "--files-from", filesFrom)
	args = append(args, "--password-file", c.SecretFile)

	src := fmt.Sprintf("rsync://replica@%s:%d/%s/", c.Host, c.Port, module)
	args = append(args, src, filepath.Clean(dstDir)+"/")

	cmd := exec.CommandContext(ctx, rsyncBin, args...)
	return cmd
}
