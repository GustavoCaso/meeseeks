package program

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// runToCompletion runs a shell script and fails if it does not finish, which is
// how a blocked output pipeline shows up.
func runToCompletion(t *testing.T, script string) Program {
	t.Helper()

	p := New("output", "sh", Args("-c", script), Async())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done, err := p.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		state := StateToString[p.State()]
		_ = p.Shutdown(time.Second)
		t.Fatalf("program still %s after 5s", state)
	}

	if got := p.State(); got != StateFinished {
		t.Fatalf("State() = %s, want finished", StateToString[got])
	}
	return p
}

func TestProgram_OutputLineLongerThanLogEntryLimitIsNotLost(t *testing.T) {
	t.Parallel()

	const lineBytes = 200_000
	script := fmt.Sprintf(
		`head -c %d /dev/zero | tr '\0' x; echo; echo after-long-line`,
		lineBytes,
	)

	stdout := runToCompletion(t, script).Stdout()

	if got := strings.Count(stdout, "x"); got != lineBytes {
		t.Fatalf("captured %d bytes of the long line, want %d", got, lineBytes)
	}
	if !strings.HasSuffix(stdout, "\nafter-long-line") {
		t.Fatal("output after the long line was not captured")
	}
}

func TestProgram_SplittingLongLineKeepsMultibyteCharactersIntact(t *testing.T) {
	t.Parallel()

	const characters = 100_000
	// The leading byte shifts two-byte characters across the entry boundary.
	script := fmt.Sprintf(`printf a; yes é | head -n %d | tr -d '\n'; echo`, characters)

	stdout := runToCompletion(t, script).Stdout()

	if !utf8.ValidString(stdout) {
		t.Fatal("captured output contains a split UTF-8 character")
	}
	if got := strings.Count(stdout, "é"); got != characters {
		t.Fatalf("captured %d characters, want %d", got, characters)
	}
}

func TestProgram_LineExactlyAtLogEntryLimitAddsNoEmptyEntry(t *testing.T) {
	t.Parallel()

	script := fmt.Sprintf(
		`head -c %d /dev/zero | tr '\0' x; echo; echo next`,
		maxLogEntryBytes,
	)

	stdout := runToCompletion(t, script).Stdout()

	if got := strings.Split(stdout, "\n"); len(got) != 2 || got[1] != "next" {
		t.Fatalf("captured %d entries, want the line followed by next", len(got))
	}
}
