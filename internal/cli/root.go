package cli

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/vbp1/pgclone/internal/clone"
	"github.com/vbp1/pgclone/internal/debug"
	"github.com/vbp1/pgclone/internal/lock"
	"github.com/vbp1/pgclone/internal/log"
	"github.com/vbp1/pgclone/internal/runctx"
	"github.com/vbp1/pgclone/internal/util/signalctx"
)

// Config holds values of CLI flags
// It will be extended later with nested sections.
// All fields are exported to allow other packages (e.g., internal/postgres) to use them.
type Config struct {
	PGHost        string
	PGPort        int
	PGUser        string
	PrimaryPGData string
	ReplicaPGData string
	ReplicaWALDir string
	SSHKey        string
	SSHUser       string
	TempWALDir    string
	Parallel      int
	Paranoid      bool
	DropExisting  bool
	Debug         bool
	KeepRunTmp    bool
	UseSlot       bool
	InsecureSSH   bool
	Progress      string
	ProgressInt   int
	Verbose       bool
}

var cfg = &Config{}

// RootCmd is the main entry point invoked from cmd/pgclone
var RootCmd = &cobra.Command{
	Use:           "pgclone",
	Short:         "Clone a PostgreSQL instance via rsync + WAL streaming (Go rewrite)",
	SilenceUsage:  true, // do not show usage on error
	SilenceErrors: true, // let RunE handle logging
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Initialize global logger once flags parsed
		slog.Debug("setting up logger")
		log.Setup(cfg.Debug, cfg.Verbose)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		slog.Info("pgclone starting")

		debug.StopIf("before-main")

		// per-run temp dir
		rc, err := runctx.New("pgclone_run_", cfg.KeepRunTmp)
		if err != nil {
			return err
		}
		slog.Debug("run temp dir", "dir", rc.Dir)
		defer func() {
			if err := rc.Cleanup(); err != nil {
				slog.Warn("cleanup temp", "err", err)
			}
		}()

		// file lock on replica PGDATA (must be provided at this point)
		if cfg.ReplicaPGData == "" {
			return fmt.Errorf("--replica-pgdata required before running")
		}
		lk := lock.New(cfg.ReplicaPGData)
		ok, err := lk.TryLock()
		if err != nil {
			return fmt.Errorf("acquire lock: %w", err)
		}
		if !ok {
			return fmt.Errorf("another pgclone process is running for %s", cfg.ReplicaPGData)
		}
		defer func() { _ = lk.Unlock() }()

		// main context with signals
		ctx, cancel, _ := signalctx.WithSignals(context.Background())
		defer cancel()
		// build orchestrator config (avoid import cycle)
		cloneCfg := &clone.Config{
			PGHost:        cfg.PGHost,
			PGPort:        cfg.PGPort,
			PGUser:        cfg.PGUser,
			PrimaryPGData: cfg.PrimaryPGData,
			ReplicaPGData: cfg.ReplicaPGData,
			ReplicaWALDir: cfg.ReplicaWALDir,
			SSHKey:        cfg.SSHKey,
			SSHUser:       cfg.SSHUser,
			InsecureSSH:   cfg.InsecureSSH,
			TempWALDir:    cfg.TempWALDir,
			UseSlot:       cfg.UseSlot,
			Parallel:      cfg.Parallel,
			Paranoid:      cfg.Paranoid,
			Verbose:       cfg.Verbose,
			KeepRunTmp:    cfg.KeepRunTmp,
			Progress:      cfg.Progress,
			ProgressInt:   cfg.ProgressInt,
		}

		if err := clone.Run(ctx, cloneCfg); err != nil {
			return err
		}

		slog.Info("pgclone finished successfully")
		return nil
	},
}

// Execute parses flags and runs the root command.
func Execute() error { return RootCmd.Execute() }

func init() {
	// Define global flags mirroring Bash version
	f := RootCmd.Flags()
	f.StringVar(&cfg.PGHost, "pghost", "", "Primary host (required)")
	f.IntVar(&cfg.PGPort, "pgport", 5432, "Primary port (default 5432)")
	f.StringVar(&cfg.PGUser, "pguser", "", "Primary user (required)")
	f.StringVar(&cfg.PrimaryPGData, "primary-pgdata", "", "Primary PGDATA path (required)")
	f.StringVar(&cfg.ReplicaPGData, "replica-pgdata", "", "Replica PGDATA path (default same as primary)")
	f.StringVar(&cfg.ReplicaWALDir, "replica-waldir", "", "Replica pg_wal path (optional)")
	f.StringVar(&cfg.SSHKey, "ssh-key", "", "SSH private key file")
	f.StringVar(&cfg.SSHUser, "ssh-user", "", "SSH user (required)")
	f.StringVar(&cfg.TempWALDir, "temp-waldir", "", "Temporary WAL directory")
	f.IntVar(&cfg.Parallel, "parallel", 0, "Number of parallel rsync jobs (default: CPU cores)")
	f.BoolVar(&cfg.Paranoid, "paranoid", false, "Enable checksum verification (slow)")
	f.BoolVar(&cfg.DropExisting, "drop-existing", false, "Remove existing data in replica PGDATA before cloning")
	f.BoolVar(&cfg.Debug, "debug", false, "Enable debug trace output")
	f.BoolVar(&cfg.KeepRunTmp, "keep-run-tmp", false, "Preserve temporary run directory")
	f.BoolVar(&cfg.UseSlot, "slot", false, "Use a temporary physical replication slot")
	f.BoolVar(&cfg.InsecureSSH, "insecure-ssh", false, "Disable strict host-key checking (NOT recommended)")
	f.StringVar(&cfg.Progress, "progress", "auto", "Progress display mode: auto|bar|plain|none")
	f.IntVar(&cfg.ProgressInt, "progress-interval", 30, "Seconds between updates in plain mode")
	f.BoolVar(&cfg.Verbose, "verbose", false, "Verbose output")

	_ = RootCmd.MarkFlagRequired("pghost")
	_ = RootCmd.MarkFlagRequired("pguser")
	_ = RootCmd.MarkFlagRequired("primary-pgdata")
	_ = RootCmd.MarkFlagRequired("ssh-user")
}
