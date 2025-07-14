package rsync

import "sort"

// Distribute splits files across N workers using lightweight ring-hop heuristic.
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
	cur := 0
	for _, f := range files {
		// choose next worker if it currently has smaller total than current worker
		nxt := (cur + 1) % workers
		if totals[nxt] < totals[cur] {
			cur = nxt
		}
		out[cur] = append(out[cur], f)
		totals[cur] += f.Size
	}
	return out
}
