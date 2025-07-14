package rsync

import (
	"fmt"
	"time"
)

// formatBytes converts byte count to human-readable string (KB, MB, etc.).
func formatBytes(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	exp, value := 0, float64(n)
	for value >= unit && exp < 5 {
		value /= unit
		exp++
	}
	suffix := []string{"KB", "MB", "GB", "TB", "PB"}[exp-1]
	return fmt.Sprintf("%.2f %s", value, suffix)
}

// Summary returns a formatted multi-line string with aggregated rsync statistics.
func (s Stats) Summary(elapsed time.Duration) string {
	if elapsed <= 0 {
		elapsed = time.Second
	}
	upRate := int64(float64(s.BytesSent) / elapsed.Seconds())
	downRate := int64(float64(s.BytesReceived) / elapsed.Seconds())

	return fmt.Sprintf("\nNumber of files: %d (reg: %d, dir: %d, link: %d)\nNumber of created files: %d (reg: %d, dir: %d)\nNumber of deleted files: %d (reg: %d, dir: %d)\nNumber of regular files transferred: %d\nTotal file size: %s\nTotal transferred file size: %s\nLiteral data: %s\nMatched data: %s\nFile list size: %s\nFile list generation time: %.3f seconds\nTotal bytes sent: %s\nTotal bytes received: %s\n\nsent %s (%s/sec) received %s (%s/sec)",
		s.NumFiles,
		s.RegFiles,
		s.DirFiles,
		s.LinkFiles,
		s.CreatedFiles,
		s.CreatedReg,
		s.CreatedDir,
		s.DeletedFiles,
		s.DeletedReg,
		s.DeletedDir,
		s.RegTransferred,
		formatBytes(s.TotalFileSize),
		formatBytes(s.TotalTransferredSize),
		formatBytes(s.LiteralData),
		formatBytes(s.MatchedData),
		formatBytes(s.FileListSize),
		s.FileListGenSeconds,
		formatBytes(s.BytesSent),
		formatBytes(s.BytesReceived),
		formatBytes(s.BytesSent),
		formatBytes(upRate),
		formatBytes(s.BytesReceived),
		formatBytes(downRate),
	)
}
