package clone

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/vbp1/pgclone/internal/postgres"
	"github.com/vbp1/pgclone/internal/rsync"
	"github.com/vbp1/pgclone/internal/ssh"
	"github.com/vbp1/pgclone/internal/wal"
)

// Orchestrator keeps state across clone steps.
type Orchestrator struct {
	cfg *Config

	conn *pgx.Conn
	recv *wal.Receiver

	rsyncPort   int
	rsyncSecret string

	rsyncDaemon *rsync.Daemon

	sshClient *ssh.Client

	startLSN string
	stopLSN  string

	tablespaces []postgres.Tablespace

	tmpDir string
}

// Close releases external resources; safe to call multiple times.
func (o *Orchestrator) Close(ctx context.Context) {
	if o.recv != nil {
		_ = o.recv.Stop()
		o.recv = nil
	}
	if o.rsyncDaemon != nil {
		_ = o.rsyncDaemon.Stop(ctx)
		o.rsyncDaemon = nil
	}
	if o.sshClient != nil {
		_ = o.sshClient.Close()
		o.sshClient = nil
	}
	if o.tmpDir != "" && !o.cfg.KeepRunTmp {
		_ = os.RemoveAll(o.tmpDir)
		o.tmpDir = ""
	}
}

// Run executes full clone pipeline (WAL receiver + rsyncd + backup start – partial).
func Run(ctx context.Context, cfg *Config) error {
	o := &Orchestrator{cfg: cfg}
	defer o.Close(ctx)
	if err := o.stepWalAndRsyncd(ctx); err != nil {
		return err
	}
	if err := o.stepBackupStart(ctx); err != nil {
		return err
	}

	if err := o.stepBackupStop(ctx); err != nil {
		return err
	}

	if err := o.stepWalFinalize(ctx); err != nil {
		return err
	}

	if err := o.stepFinalChecks(ctx); err != nil {
		return err
	}

	slog.Info("clone pipeline completed – replica ready")
	return nil
}

// stepWalAndRsyncd starts pg_receivewal, waits replication, then launches rsyncd on primary.
func (o *Orchestrator) stepWalAndRsyncd(ctx context.Context) error {
	// tmp dir for WAL
	walDir := o.cfg.TempWALDir
	if walDir == "" {
		d, err := os.MkdirTemp("", "pgclone_wal_*")
		if err != nil {
			return err
		}
		walDir = d
		o.tmpDir = d
	}

	appName := fmt.Sprintf("pgclone-%d", time.Now().UnixNano())

	o.recv = &wal.Receiver{
		Host:    o.cfg.PGHost,
		Port:    o.cfg.PGPort,
		User:    o.cfg.PGUser,
		Dir:     walDir,
		Slot:    o.cfg.SlotName,
		Verbose: o.cfg.Verbose,
		AppName: appName,
	}
	if err := o.recv.Start(ctx); err != nil {
		return err
	}
	slog.Info("pg_receivewal started", "dir", walDir)

	// single pgx connection for backup start/stop
	conn, err := pgx.Connect(ctx, fmt.Sprintf("host=%s port=%d user=%s sslmode=disable", o.cfg.PGHost, o.cfg.PGPort, o.cfg.PGUser))
	if err != nil {
		_ = o.recv.Stop()
		return err
	}
	o.conn = conn

	if err := postgres.WaitReplicationStarted(ctx, o.conn, appName, 60*time.Second); err != nil {
		return err
	}
	slog.Info("replication started")

	// fetch tablespaces
	tsRows, err := o.conn.Query(ctx, `SELECT oid, pg_tablespace_location(oid)
                                       FROM pg_tablespace
                                       WHERE spcname NOT IN ('pg_default','pg_global')`)
	if err != nil {
		return err
	}
	for tsRows.Next() {
		var oid uint32
		var loc string
		if err := tsRows.Scan(&oid, &loc); err != nil {
			return err
		}
		o.tablespaces = append(o.tablespaces, postgres.Tablespace{Oid: oid, Location: loc})
	}
	_ = tsRows.Err()

	// build modules map
	modules := map[string]string{
		"pgdata": o.cfg.PrimaryPGData,
		"base":   filepath.Join(o.cfg.PrimaryPGData, "base"),
	}
	for _, t := range o.tablespaces {
		modules[fmt.Sprintf("spc_%d", t.Oid)] = t.Location
	}

	// ssh client
	sshClient, err := ssh.Dial(ctx, ssh.Config{
		User:     o.cfg.SSHUser,
		Host:     o.cfg.PGHost,
		KeyPath:  o.cfg.SSHKey,
		Insecure: o.cfg.InsecureSSH,
		Timeout:  10 * time.Second,
	})
	if err != nil {
		return err
	}
	// bootstrap
	daemon, err := rsync.StartRemote(ctx, sshClient, rsync.BootstrapOptions{
		Modules: modules,
		MaxConn: o.cfg.Parallel * 4,
	})
	if err != nil {
		return err
	}
	o.rsyncPort, o.rsyncSecret = daemon.Port, daemon.Secret
	o.rsyncDaemon = daemon
	o.sshClient = sshClient
	slog.Info("rsyncd ready", "port", daemon.Port)
	return nil
}

// listModuleFiles returns file listing for a module via rsync --list-only.
func listModuleFiles(ctx context.Context, cfg rsync.Config, module string) ([]rsync.FileInfo, error) {
	args := []string{"--recursive", "--list-only", "--password-file", cfg.SecretFile}
	src := fmt.Sprintf("rsync://replica@%s:%d/%s/", cfg.Host, cfg.Port, module)
	args = append(args, src)
	cmd := exec.CommandContext(ctx, "rsync", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("rsync list-only: %w", err)
	}
	files, err := rsync.ParseList(bytes.NewReader(out))
	if err != nil {
		return nil, err
	}
	return files, nil
}

// stepBackupStart calls pg_backup_start and stores LSN.
func (o *Orchestrator) stepBackupStart(ctx context.Context) error {
	if err := o.conn.QueryRow(ctx, `SELECT pg_backup_start('pgclone', true)`).Scan(&o.startLSN); err != nil {
		return fmt.Errorf("pg_backup_start: %w", err)
	}
	slog.Info("backup started", "start_lsn", o.startLSN)

	// initial rsync PGDATA excluding base/pg_wal etc.
	// Ensure replica data directory exists (mkdir -p)
	if err := os.MkdirAll(o.cfg.ReplicaPGData, 0o755); err != nil {
		return fmt.Errorf("create replica data dir: %w", err)
	}
	secretFile := filepath.Join(os.TempDir(), "pgclone_rsync_pass")
	if err := os.WriteFile(secretFile, []byte(o.rsyncSecret), 0o600); err != nil {
		return err
	}

	rcfg := rsync.Config{
		Host:       o.cfg.PGHost,
		Port:       o.rsyncPort,
		SecretFile: secretFile,
		Checksum:   o.cfg.Paranoid,
		Verbose:    o.cfg.Verbose,
	}

	// Build command for initial copy of entire PGDATA (excluding pg_wal & base)
	rsyncArgs := []string{"-a", "--delete", "--stats"}
	if rcfg.Checksum {
		rsyncArgs = append(rsyncArgs, "--checksum")
	}
	if rcfg.Verbose {
		rsyncArgs = append(rsyncArgs, "--human-readable")
	}
	// exclusions identical to Bash implementation
	excludes := []string{
		"pg_wal/", "base/", "postmaster.pid", "postmaster.opts", "pg_replslot/", "pg_dynshmem/", "pg_notify/", "pg_serial/", "pg_snapshots/", "pg_stat_tmp/", "pg_subtrans/", "pgsql_tmp*", "pg_internal.init",
	}
	for _, ex := range excludes {
		rsyncArgs = append(rsyncArgs, "--exclude", ex)
	}
	rsyncArgs = append(rsyncArgs, "--password-file", secretFile)

	src := fmt.Sprintf("rsync://replica@%s:%d/pgdata/", rcfg.Host, rcfg.Port)
	dst := filepath.Clean(o.cfg.ReplicaPGData) + "/"
	rsyncArgs = append(rsyncArgs, src, dst)

	cmd := exec.CommandContext(ctx, "rsync", rsyncArgs...)

	slog.Info("running initial rsync pgdata")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("initial rsync: %w\n%s", err, string(out))
	}
	slog.Info("initial rsync done")

	// ensure required empty directories that were excluded from rsync
	runtimeDirs := []string{"pg_replslot", "pg_dynshmem", "pg_notify", "pg_serial", "pg_snapshots", "pg_stat_tmp", "pg_subtrans"}
	for _, d := range runtimeDirs {
		path := filepath.Join(o.cfg.ReplicaPGData, d)
		_ = os.MkdirAll(path, 0o700)
	}

	// --- parallel rsync of base ---
	startTransfer := time.Now()
	totalStats := rsync.Stats{}
	baseFiles, err := listModuleFiles(ctx, rcfg, "base")
	if err != nil {
		return err
	}
	slog.Info("base file list", "count", len(baseFiles))

	baseDst := filepath.Join(o.cfg.ReplicaPGData, "base")
	if err := os.MkdirAll(baseDst, 0o755); err != nil {
		return err
	}

	showBar := o.cfg.Progress == "bar" || (o.cfg.Progress == "auto" && o.cfg.Verbose)
	stats, err := rsync.RunParallel(ctx, rcfg, "base", o.cfg.Parallel, baseFiles, baseDst, showBar, o.cfg.Progress, o.cfg.ProgressInt)
	if err != nil {
		return err
	}
	slog.Info("base rsync done", "files", stats.NumFiles, "bytes", stats.TotalTransferredSize)
	totalStats = totalStats.Add(stats)

	// --- tablespaces ---
	for _, t := range o.tablespaces {
		mod := fmt.Sprintf("spc_%d", t.Oid)
		spcFiles, err := listModuleFiles(ctx, rcfg, mod)
		if err != nil {
			return err
		}
		slog.Info("tablespace list", "oid", t.Oid, "count", len(spcFiles))
		if len(spcFiles) == 0 {
			continue
		}
		if err := os.MkdirAll(t.Location, 0o755); err != nil {
			return err
		}
		st, err := rsync.RunParallel(ctx, rcfg, mod, o.cfg.Parallel, spcFiles, t.Location, showBar, o.cfg.Progress, o.cfg.ProgressInt)
		if err != nil {
			return err
		}
		slog.Info("tablespace rsync done", "oid", t.Oid, "bytes", st.TotalTransferredSize)
		totalStats = totalStats.Add(st)
	}

	// Print aggregated stats similar to bash implementation
	slog.Info("rsync aggregate stats", "elapsed_sec", time.Since(startTransfer).Seconds())
	fmt.Println(totalStats.Summary(time.Since(startTransfer)))

	return nil
}

// stepBackupStop finishes backup, fetches control files and stop LSN.
func (o *Orchestrator) stepBackupStop(ctx context.Context) error {
	var stopLSN, labelB64, mapB64 string
	if err := o.conn.QueryRow(ctx, `SELECT lsn,
          translate(encode(labelfile::bytea,  'base64'), E'\n', ''),
          translate(encode(spcmapfile::bytea, 'base64'), E'\n', '')
          FROM pg_backup_stop(true)`).Scan(&stopLSN, &labelB64, &mapB64); err != nil {
		return fmt.Errorf("pg_backup_stop: %w", err)
	}
	o.stopLSN = stopLSN
	slog.Info("backup stopped", "stop_lsn", stopLSN)

	// write backup_label & tablespace_map
	labelBytes, _ := base64.StdEncoding.DecodeString(labelB64)
	if err := os.WriteFile(filepath.Join(o.cfg.ReplicaPGData, "backup_label"), labelBytes, 0o644); err != nil {
		return err
	}
	if mapB64 != "" {
		mapBytes, _ := base64.StdEncoding.DecodeString(mapB64)
		_ = os.WriteFile(filepath.Join(o.cfg.ReplicaPGData, "tablespace_map"), mapBytes, 0o644)
	}

	// fetch pg_control via ssh
	ctrlPath := filepath.Join(o.cfg.PrimaryPGData, "global", "pg_control")
	destCtrl := filepath.Join(o.cfg.ReplicaPGData, "global", "pg_control")
	// Ensure destination directory exists (mkdir -p semantics)
	_ = os.MkdirAll(filepath.Dir(destCtrl), 0o755)
	cmd := fmt.Sprintf("cat '%s'", ctrlPath)
	data, err := o.sshClient.Output(ctx, cmd)
	if err != nil {
		return fmt.Errorf("fetch pg_control: %w", err)
	}
	if err := os.WriteFile(destCtrl, data, 0o600); err != nil {
		return err
	}
	return nil
}

// stepWalFinalize waits for WAL, stops receiver, moves files, renames partial.
func (o *Orchestrator) stepWalFinalize(ctx context.Context) error {
	// compute wal filename
	var walFile string
	if err := o.conn.QueryRow(ctx, `SELECT pg_walfile_name($1)`, o.stopLSN).Scan(&walFile); err != nil {
		return err
	}
	walDir := o.recv.Dir
	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(walDir, walFile)); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wal %s not received", walFile)
		}
		time.Sleep(2 * time.Second)
	}

	// stop receiver
	if err := o.recv.Stop(); err != nil {
		slog.Warn("receiver stop", "err", err)
	}

	// move files to replica WAL dir
	dstWal := o.cfg.ReplicaWALDir
	if dstWal == "" {
		dstWal = filepath.Join(o.cfg.ReplicaPGData, "pg_wal")
	}
	_ = os.MkdirAll(dstWal, 0o700)

	entries, _ := os.ReadDir(walDir)
	for _, e := range entries {
		src := filepath.Join(walDir, e.Name())
		dst := filepath.Join(dstWal, e.Name())
		if err := os.Rename(src, dst); err != nil {
			// likely cross-device link; fallback to copy
			if err := copyFile(src, dst); err != nil {
				return err
			}
			_ = os.Remove(src)
		}
	}

	// rename last .partial
	partials, _ := filepath.Glob(filepath.Join(dstWal, "*.partial"))
	if len(partials) > 0 {
		sort.Strings(partials)
		last := partials[len(partials)-1]
		_ = os.Rename(last, strings.TrimSuffix(last, ".partial"))
	}

	return nil
}

// stepFinalChecks validates resulting replica, fixes permissions and prints summary.
func (o *Orchestrator) stepFinalChecks(ctx context.Context) error {
	// essential files
	need := []string{"PG_VERSION", "postgresql.conf", "pg_hba.conf"}
	for _, f := range need {
		if _, err := os.Stat(filepath.Join(o.cfg.ReplicaPGData, f)); err != nil {
			return fmt.Errorf("missing %s: %w", f, err)
		}
	}

	walDir := o.cfg.ReplicaWALDir
	if walDir == "" {
		walDir = filepath.Join(o.cfg.ReplicaPGData, "pg_wal")
	}
	files, _ := filepath.Glob(filepath.Join(walDir, "[0-9A-F]*"))
	if len(files) == 0 {
		return fmt.Errorf("no WAL files in %s", walDir)
	}

	// chmod
	_ = os.Chmod(o.cfg.ReplicaPGData, 0o700)
	_ = os.Chmod(walDir, 0o700)

	slog.Info("final validation ok", "wal_files", len(files))
	return nil
}

// ### helper: copyFile src->dst preserving perms, used when os.Rename crosses fs boundary.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := in.Close(); err != nil {
			slog.Warn("copyFile: failed to close source", "err", err)
		}
	}()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// copy file mode
	if info, err := os.Stat(src); err == nil {
		_ = os.Chmod(dst, info.Mode())
	}
	return nil
}
