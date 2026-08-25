package buildinfo

import (
	"strings"
	"testing"
)

// TestStringIncludesAllThreeFields guards against a future refactor
// silently dropping one of version/commit/date from the printed line
// — the only thing `tickets --version` and the startup log line
// actually promise.
func TestStringIncludesAllThreeFields(t *testing.T) {
	oldVersion, oldCommit, oldDate := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = oldVersion, oldCommit, oldDate })

	Version, Commit, Date = "v1.2.3", "abc1234", "2026-01-01T00:00:00Z"
	got := String()
	for _, want := range []string{"v1.2.3", "abc1234", "2026-01-01T00:00:00Z"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, want it to contain %q", got, want)
		}
	}
}

// TestDefaultsAreNeverEmpty guards ADR 0010's "Go-only contributor"
// guarantee: a bare `go build` (no ldflags) must never produce an
// empty version/commit/date string, only the documented placeholders.
func TestDefaultsAreNeverEmpty(t *testing.T) {
	if Version == "" || Commit == "" || Date == "" {
		t.Errorf("package-level defaults = %q/%q/%q, want none empty", Version, Commit, Date)
	}
}
