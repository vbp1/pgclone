package disk

import (
	"fmt"
	"syscall"
)

// Space holds information about free and total bytes.
// Values are in bytes.
// On Linux, Statfs uses fragment size in Bsize.
type Space struct {
	Free  uint64
	Total uint64
}

// FreeBytes returns available (for unprivileged user) and total bytes on filesystem containing path.
func FreeBytes(path string) (Space, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Space{}, fmt.Errorf("statfs %s: %w", path, err)
	}
	free := st.Bavail * uint64(st.Bsize)
	total := st.Blocks * uint64(st.Bsize)
	return Space{Free: free, Total: total}, nil
}

// EnsureSpace checks that each path in need has at least required bytes free.
// Keys: directory paths; value: required bytes.
func EnsureSpace(need map[string]uint64) error {
	for p, req := range need {
		sp, err := FreeBytes(p)
		if err != nil {
			return err
		}
		if sp.Free < req {
			return fmt.Errorf("insufficient space on %s: free %.2f MB, need %.2f MB", p, bytesToMB(sp.Free), bytesToMB(req))
		}
	}
	return nil
}

func bytesToMB(b uint64) float64 {
	return float64(b) / (1024 * 1024)
}
