package clone

// Config collects parameters required by the clone orchestrator.
// It is a subset/superset of CLI flags but lives in a standalone package to avoid import cycles.
type Config struct {
	PGHost        string
	PGPort        int
	PGUser        string
	PrimaryPGData string
	ReplicaPGData string
	ReplicaWALDir string

	SSHKey      string
	SSHUser     string
	InsecureSSH bool

	TempWALDir string
	UseSlot    bool
	SlotName   string // optional preset; if empty and UseSlot, Orchestrator will generate

	Parallel int
	Paranoid bool
	Verbose  bool

	KeepRunTmp bool

	Progress    string
	ProgressInt int
}
