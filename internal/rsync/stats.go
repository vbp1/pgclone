package rsync

import (
	"bufio"
	"regexp"
	"strconv"
	"strings"
)

// Stats aggregated from rsync --stats output.
type Stats struct {
	NumFiles             int64
	CreatedFiles         int64
	CreatedReg           int64
	CreatedDir           int64
	DeletedFiles         int64
	DeletedReg           int64
	DeletedDir           int64
	RegTransferred       int64
	TotalFileSize        int64
	TotalTransferredSize int64
	LiteralData          int64
	MatchedData          int64
	RegFiles             int64 // from Number of files (reg)
	DirFiles             int64 // from Number of files (dir)
	LinkFiles            int64 // from Number of files (link/sym)
	FileListSize         int64
	FileListGenSeconds   float64
	BytesSent            int64
	BytesReceived        int64
}

var (
	reNumFiles         = regexp.MustCompile(`^\s*Number of files:\s+([0-9,]+)(?:\s*\(([^)]+)\))?`)
	reCreatedFiles     = regexp.MustCompile(`^\s*Number of created files:\s+([0-9,]+)(?:\s*\(([^)]+)\))?`)
	reDeletedFiles     = regexp.MustCompile(`^\s*Number of deleted files:\s+([0-9,]+)(?:\s*\(([^)]+)\))?`)
	reRegTransferred   = regexp.MustCompile(`^\s*Number of regular files transferred:\s+([0-9,]+)`)
	reTotalFileSize    = regexp.MustCompile(`^\s*Total file size:\s+([0-9.,A-Za-z]+)`)
	reTotalTransferred = regexp.MustCompile(`^\s*Total transferred file size:\s+([0-9.,A-Za-z]+)`)
	reLiteral          = regexp.MustCompile(`^\s*Literal data:\s+([0-9.,A-Za-z]+)`)
	reMatched          = regexp.MustCompile(`^\s*Matched data:\s+([0-9.,A-Za-z]+)`)
	reBytesSent        = regexp.MustCompile(`^\s*Total bytes sent:\s+([0-9.,A-Za-z]+)`)
	reFileListSize     = regexp.MustCompile(`^\s*File list size:\s+([0-9.,A-Za-z]+)`)
	reFileListGenTime  = regexp.MustCompile(`^\s*File list generation time:\s+([0-9.,]+) seconds?`)
	reBytesReceived    = regexp.MustCompile(`^\s*Total bytes received:\s+([0-9.,A-Za-z]+)`)
)

// ParseStats parses rsync --stats output from scanner.
func ParseStats(sc *bufio.Scanner) (Stats, error) {
	var s Stats
	for sc.Scan() {
		line := sc.Text()
		switch {
		case reNumFiles.MatchString(line):
			m := reNumFiles.FindStringSubmatch(line)
			s.NumFiles = toInt(m[1])
			if len(m) > 2 && m[2] != "" {
				// parse categories e.g. "reg: 16, dir: 2, link: 1"
				parts := strings.Split(m[2], ",")
				for _, p := range parts {
					kv := strings.Split(strings.TrimSpace(p), ":")
					if len(kv) != 2 {
						continue
					}
					key := strings.TrimSpace(kv[0])
					val := toInt(kv[1])
					switch key {
					case "reg":
						s.RegFiles = val
					case "dir":
						s.DirFiles = val
					case "link", "sym":
						s.LinkFiles = val
					}
				}
			}
		case reCreatedFiles.MatchString(line):
			m := reCreatedFiles.FindStringSubmatch(line)
			s.CreatedFiles = toInt(m[1])
			if len(m) > 2 && m[2] != "" {
				parts := strings.Split(m[2], ",")
				for _, p := range parts {
					kv := strings.Split(strings.TrimSpace(p), ":")
					if len(kv) != 2 {
						continue
					}
					key := strings.TrimSpace(kv[0])
					val := toInt(kv[1])
					switch key {
					case "reg", "regular files", "regular":
						s.CreatedReg = val
					case "dir", "directories":
						s.CreatedDir = val
					}
				}
			}
		case reDeletedFiles.MatchString(line):
			m := reDeletedFiles.FindStringSubmatch(line)
			s.DeletedFiles = toInt(m[1])
			if len(m) > 2 && m[2] != "" {
				parts := strings.Split(m[2], ",")
				for _, p := range parts {
					kv := strings.Split(strings.TrimSpace(p), ":")
					if len(kv) != 2 {
						continue
					}
					key := strings.TrimSpace(kv[0])
					val := toInt(kv[1])
					switch key {
					case "reg", "regular files", "regular", "file", "files":
						s.DeletedReg = val
					case "dir", "directories":
						s.DeletedDir = val
					}
				}
			}
		case reRegTransferred.MatchString(line):
			s.RegTransferred = toInt(reRegTransferred.FindStringSubmatch(line)[1])
		case reTotalFileSize.MatchString(line):
			s.TotalFileSize = toBytes(reTotalFileSize.FindStringSubmatch(line)[1])
		case reTotalTransferred.MatchString(line):
			s.TotalTransferredSize = toBytes(reTotalTransferred.FindStringSubmatch(line)[1])
		case reLiteral.MatchString(line):
			s.LiteralData = toBytes(reLiteral.FindStringSubmatch(line)[1])
		case reMatched.MatchString(line):
			s.MatchedData = toBytes(reMatched.FindStringSubmatch(line)[1])
		case reBytesSent.MatchString(line):
			s.BytesSent = toBytes(reBytesSent.FindStringSubmatch(line)[1])
		case reFileListSize.MatchString(line):
			s.FileListSize = toBytes(reFileListSize.FindStringSubmatch(line)[1])
		case reFileListGenTime.MatchString(line):
			v := reFileListGenTime.FindStringSubmatch(line)[1]
			f, _ := strconv.ParseFloat(strings.ReplaceAll(v, ",", "."), 64)
			if f > s.FileListGenSeconds {
				s.FileListGenSeconds = f
			}
		case reBytesReceived.MatchString(line):
			s.BytesReceived = toBytes(reBytesReceived.FindStringSubmatch(line)[1])
		}
	}
	return s, sc.Err()
}

func toInt(s string) int64 {
	v, _ := strconv.ParseInt(cleanNum(s), 10, 64)
	return v
}

// toBytes converts size strings like "1234", "2.3K", "1.2 MiB" to bytes
func toBytes(s string) int64 {
	if s == "" {
		return 0
	}
	s = strings.TrimSpace(s)

	// Check if string contains unit suffixes
	hasUnit := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && c != '.' && c != ',' && c != ' ' {
			hasUnit = true
			break
		}
	}

	if hasUnit {
		return parseHumanSize(s)
	}

	// Plain number - use existing logic
	return toInt(s)
}

// parseHumanSize converts "2.3K", "1.2MiB", "100G" to bytes
func parseHumanSize(s string) int64 {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, ",", "")

	// Find first non-numeric character (except decimal point)
	i := 0
	for ; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && c != '.' {
			break
		}
	}

	numPart := s[:i]
	unitPart := strings.ToUpper(s[i:])

	f, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0
	}

	var multiplier float64 = 1
	switch {
	case strings.HasPrefix(unitPart, "K"):
		multiplier = 1 << 10 // 1024
	case strings.HasPrefix(unitPart, "M"):
		multiplier = 1 << 20 // 1048576
	case strings.HasPrefix(unitPart, "G"):
		multiplier = 1 << 30
	case strings.HasPrefix(unitPart, "T"):
		multiplier = 1 << 40
	case strings.HasPrefix(unitPart, "P"):
		multiplier = 1 << 50
	}

	return int64(f * multiplier)
}

func cleanNum(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			out = append(out, r)
		}
	}
	return string(out)
}
