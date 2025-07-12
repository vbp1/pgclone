package fs

import (
	"os"
	"testing"
)

func TestMkdirPAndCleanup(t *testing.T) {
	tmp := t.TempDir()
	nested := tmp + "/a/b/c"
	if err := MkdirP(nested); err != nil {
		t.Fatalf("MkdirP failed: %v", err)
	}
	// create file inside
	f := nested + "/file.txt"
	if err := os.WriteFile(f, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := CleanupDir(tmp); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	entries, _ := os.ReadDir(tmp)
	if len(entries) != 0 {
		t.Fatalf("expected dir empty after cleanup, got %d entries", len(entries))
	}
}
