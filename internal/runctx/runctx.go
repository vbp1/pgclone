package runctx

import (
	"fmt"
	"os"
	"path/filepath"
)

// RunCtx manages a per-run temporary directory.
type RunCtx struct {
	Dir        string
	keepOnExit bool
}

// New creates directory under system temp with prefix.
func New(prefix string, keep bool) (*RunCtx, error) {
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		return nil, err
	}
	return &RunCtx{Dir: dir, keepOnExit: keep}, nil
}

// Cleanup removes directory unless keepOnExit=true.
func (r *RunCtx) Cleanup() error {
	if r.keepOnExit {
		return nil
	}
	return os.RemoveAll(r.Dir)
}

// Path joins run dir with subpath.
func (r *RunCtx) Path(elem ...string) string {
	parts := append([]string{r.Dir}, elem...)
	return filepath.Join(parts...)
}

func (r *RunCtx) String() string { return fmt.Sprintf("RunCtx(%s)", r.Dir) }
