package rsync

import "sort"

// Distribute splits files across N workers using improved hybrid distribution algorithm.
// It assumes files are not sorted; they will be sorted by size descending internally.
// Returns slice len==workers; each element is slice of FileInfo for that worker.
func Distribute(files []FileInfo, workers int) [][]FileInfo {
	if workers <= 0 {
		return nil
	}
	out := make([][]FileInfo, workers)
	if len(files) == 0 {
		return out
	}

	// sort by size descending
	sort.Slice(files, func(i, j int) bool { return files[i].Size > files[j].Size })

	totals := make([]int64, workers)
	
	// Hybrid algorithm: best-fit for large files (>1GB), round-robin for small files
	// This provides better load balancing for PostgreSQL databases with mixed file sizes
	threshold := int64(1024 * 1024 * 1024) // 1GB
	cur := 0 // for round-robin
	
	for _, f := range files {
		if f.Size > threshold {
			// Best-fit for large files: assign to worker with minimum total size
			minWorker := 0
			for i := 1; i < workers; i++ {
				if totals[i] < totals[minWorker] {
					minWorker = i
				}
			}
			out[minWorker] = append(out[minWorker], f)
			totals[minWorker] += f.Size
		} else {
			// Round-robin for small files to avoid scanning overhead
			out[cur] = append(out[cur], f)
			totals[cur] += f.Size
			cur = (cur + 1) % workers
		}
	}
	return out
}
