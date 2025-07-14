package rsync_test

import (
	"strings"
	"testing"

	"github.com/vbp1/pgclone/internal/rsync"
)

func TestParseList(t *testing.T) {
	sample := `-rw-r--r--        1 4096 2024/01/01 10:00:00 base/1/123
-rw-r--r--        1 1,048 2024/01/01 10:00:00 base/1/456
` // second size with comma
	files, err := rsync.ParseList(strings.NewReader(sample))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("want 2 files, got %d", len(files))
	}
	if files[0].Size != 4096 || files[0].Path != "base/1/123" {
		t.Fatalf("file0 mismatch: %+v", files[0])
	}
	if files[1].Size != 1048 {
		t.Fatalf("file1 size parse failed: %+v", files[1])
	}
}
