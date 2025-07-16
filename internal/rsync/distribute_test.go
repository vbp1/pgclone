package rsync_test

import (
	"fmt"
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

func TestDistributeHybridImprovement(t *testing.T) {
	// Create test data that simulates PostgreSQL database files
	var files []rsync.FileInfo
	
	// Add large files (>1GB) - should use best-fit
	for i := 0; i < 10; i++ {
		files = append(files, rsync.FileInfo{
			Path: fmt.Sprintf("large_table_%d", i),
			Size: int64(2+i) * 1024 * 1024 * 1024, // 2-11GB
		})
	}
	
	// Add small files (<1GB) - should use round-robin
	for i := 0; i < 96; i++ {
		files = append(files, rsync.FileInfo{
			Path: fmt.Sprintf("small_file_%d", i),
			Size: int64(10+i) * 1024 * 1024, // 10-106MB
		})
	}
	
	workers := 8
	buckets := rsync.Distribute(files, workers)
	
	// Calculate distribution balance
	workerSizes := make([]int64, workers)
	for i, bucket := range buckets {
		for _, f := range bucket {
			workerSizes[i] += f.Size
		}
	}
	
	// Find min and max
	minSize := workerSizes[0]
	maxSize := workerSizes[0]
	var totalSize int64
	
	for i := 0; i < workers; i++ {
		if workerSizes[i] < minSize {
			minSize = workerSizes[i]
		}
		if workerSizes[i] > maxSize {
			maxSize = workerSizes[i]
		}
		totalSize += workerSizes[i]
	}
	
	avgSize := totalSize / int64(workers)
	imbalance := float64(maxSize-minSize) / float64(avgSize) * 100
	
	// With improved algorithm, imbalance should be reasonable (<100%)
	if imbalance > 100.0 {
		t.Errorf("Distribution imbalance too high: %.2f%% (max: %.2f GB, min: %.2f GB)", 
			imbalance, 
			float64(maxSize)/(1024*1024*1024), 
			float64(minSize)/(1024*1024*1024))
	}
	
	t.Logf("Distribution quality: %.2f%% imbalance (avg: %.2f GB, range: %.2f-%.2f GB)",
		imbalance,
		float64(avgSize)/(1024*1024*1024),
		float64(minSize)/(1024*1024*1024),
		float64(maxSize)/(1024*1024*1024))
}
