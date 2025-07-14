package lock

import "testing"

func TestFileLock(t *testing.T) {
	l1 := New("/tmp/pgdata_test")
	ok, err := l1.TryLock()
	if err != nil || !ok {
		t.Fatalf("first lock failed")
	}
	defer func() { _ = l1.Unlock() }()

	l2 := New("/tmp/pgdata_test")
	ok, err = l2.TryLock()
	if err != nil {
		t.Fatalf("second lock error: %v", err)
	}
	if ok {
		t.Fatalf("lock should be held by first process")
	}
}
