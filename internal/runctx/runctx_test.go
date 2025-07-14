package runctx

import (
	"os"
	"testing"
)

func TestRunCtxLifecycle(t *testing.T) {
	rc, err := New("pgclone_test", false)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := rc.Cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	// directory should be gone
	if _, err := os.Stat(rc.Dir); !os.IsNotExist(err) {
		t.Fatalf("dir still exists")
	}
}
