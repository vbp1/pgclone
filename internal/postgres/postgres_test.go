package postgres

import "testing"

func TestPrettyBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{500, "500 bytes"},
		{1024, "1.00 kB"},
		{1024*1024 + 512*1024, "1.50 MB"},
	}
	for _, c := range cases {
		got := PrettyBytes(c.in)
		if got != c.want {
			t.Errorf("PrettyBytes(%d)=%s, want %s", c.in, got, c.want)
		}
	}
}
