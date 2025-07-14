package lock

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// FileLock wraps gofrs/flock for PGDATA path.
type FileLock struct {
	fl   *flock.Flock
	path string
}

// New returns lock at /tmp/pgclone_<hash>.lock.
func New(pgdata string) *FileLock {
	abs := filepath.Clean(pgdata)
	sum := sha256.Sum256([]byte(abs))
	name := fmt.Sprintf("/tmp/pgclone_%s.lock", hex.EncodeToString(sum[:8]))
	return &FileLock{fl: flock.New(name), path: name}
}

// TryLock attempts non-blocking lock.
func (l *FileLock) TryLock() (bool, error) {
	return l.fl.TryLock()
}

// Unlock releases.
func (l *FileLock) Unlock() error {
	// Release the OS-level lock first.
	if err := l.fl.Unlock(); err != nil {
		return err
	}
	// Best-effort cleanup: remove the lock file so it does not linger in /tmp.
	// Ignore any error (e.g. if another process already removed it).
	_ = os.Remove(l.path)
	return nil
}
