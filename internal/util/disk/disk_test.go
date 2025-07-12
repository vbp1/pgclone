package disk

import "testing"

func TestFreeBytes(t *testing.T) {
	space, err := FreeBytes("./")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if space.Free == 0 || space.Total == 0 {
		t.Fatalf("free or total cannot be zero: %+v", space)
	}
}

func TestEnsureSpace(t *testing.T) {
	tmpDir := t.TempDir()
	// require 1 byte — should succeed
	if err := EnsureSpace(map[string]uint64{tmpDir: 1}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
