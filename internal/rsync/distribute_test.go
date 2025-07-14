package rsync_test

import (
	"testing"

	"github.com/vbp1/pgclone/internal/rsync"
)

func TestDistributeBalances(t *testing.T) {
	files := make([]rsync.FileInfo, 10)
	for i := range files {
		files[i] = rsync.FileInfo{Size: int64(100 * (i + 1)), Path: "f"}
	}
	out := rsync.Distribute(files, 3)
	if len(out) != 3 {
		t.Fatalf("expected 3 workers, got %d", len(out))
	}
	var totals [3]int64
	for i, w := range out {
		for _, f := range w {
			totals[i] += f.Size
		}
	}
	// difference between max and min should be small (< largest file)
	max, min := totals[0], totals[0]
	for _, v := range totals[1:] {
		if v > max {
			max = v
		}
		if v < min {
			min = v
		}
	}
	if max-min > 100*10 { // largest file size
		t.Fatalf("load imbalance too high: totals=%v", totals)
	}
}
