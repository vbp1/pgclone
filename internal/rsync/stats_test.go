package rsync

import (
	"bufio"
	"strings"
	"testing"
)

const sample = `Number of files: 10
Number of regular files transferred: 2
Total file size: 5,120 bytes
Total transferred file size: 4,096 bytes
Literal data: 4,096 bytes
Matched data: 0 bytes
Total bytes sent: 2.00K
Total bytes received: 80`

func TestParseStats(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader(sample))
	st, err := ParseStats(sc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if st.NumFiles != 10 || st.RegTransferred != 2 || st.TotalFileSize != 5120 || st.BytesSent == 0 {
		t.Fatalf("unexpected parsed stats: %+v", st)
	}
}
