package rsync

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// FileInfo represents a single file entry produced by `rsync --list-only`.
type FileInfo struct {
	Size int64  // bytes
	Path string // relative path inside module
}

// ParseList parses rsync --list-only output.
// It expects lines like:
// -rw-r--r--        4096 2024/01/01 00:00:00 path/to/file
// Some rsync versions include an additional numeric column (hard link count).
func ParseList(r io.Reader) ([]FileInfo, error) {
	var out []FileInfo
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "-") { // only regular files
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue // malformed
		}
		// size field is either fields[1] (old format) or fields[2] (when link count present)
		sizeFieldIdx := 1
		if _, err := strconv.Atoi(fields[1]); err == nil && len(fields) >= 6 {
			// if fields[1] looks like link count, size is probably fields[2]
			// link count is small (<10) while size may be big; but we can't be sure.
			// Heuristic: if next field parses after cleaning separators, use it.
			if s, err2 := strconv.ParseInt(cleanNumber(fields[2]), 10, 64); err2 == nil {
				sizeFieldIdx = 2
				_ = s // just confirmation
			}
		}
		sizeVal, err := strconv.ParseInt(cleanNumber(fields[sizeFieldIdx]), 10, 64)
		if err != nil {
			continue
		}
		path := fields[len(fields)-1]
		out = append(out, FileInfo{Size: sizeVal, Path: path})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// cleanNumber removes thousand separators (comma, dot) from numbers.
func cleanNumber(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			b = append(b, c)
		}
	}
	return string(b)
}
