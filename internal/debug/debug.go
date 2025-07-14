package debug

import (
	"fmt"
	"os"
)

// StopIf blocks indefinitely if the environment variable PGCLONE_TEST_STOP
// equals the provided label. It prints a marker line to stderr so tests can
// wait until the exact stop point is reached before sending signals.
func StopIf(label string) {
	if os.Getenv("PGCLONE_TEST_STOP") != label {
		return
	}
	fmt.Fprintf(os.Stderr, "TEST_stop_point_%s\n", label)
	select {}
}
