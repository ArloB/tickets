package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestAnsiColorWrapsOnlyWhenEnabled(t *testing.T) {
	if got := ansiColor("31", "x", true); got != "\x1b[31mx\x1b[0m" {
		t.Errorf("ansiColor(enabled) = %q, want the wrapped escape sequence", got)
	}
	if got := ansiColor("31", "x", false); got != "x" {
		t.Errorf("ansiColor(disabled) = %q, want the plain string unchanged", got)
	}
}

func TestColorStatusMapping(t *testing.T) {
	cases := []struct {
		status string
		code   string // "" means left plain
	}{
		{"done", "32"},
		{"accepted", "32"},
		{"blocked", "31"},
		{"cancelled", "31"},
		{"rejected", "31"},
		{"in_progress", "33"},
		{"review", "33"},
		{"backlog", ""},
		{"proposed", ""},
		{"some-future-status", ""},
	}
	for _, c := range cases {
		got := colorStatus(c.status, true)
		want := c.status
		if c.code != "" {
			want = "\x1b[" + c.code + "m" + c.status + "\x1b[0m"
		}
		if got != want {
			t.Errorf("colorStatus(%q, true) = %q, want %q", c.status, got, want)
		}
		if plain := colorStatus(c.status, false); plain != c.status {
			t.Errorf("colorStatus(%q, false) = %q, want the plain status unchanged", c.status, plain)
		}
	}
}

func TestColorPriorityMapping(t *testing.T) {
	cases := []struct {
		priority string
		code     string
	}{
		{"critical", "31"},
		{"high", "33"},
		{"medium", ""},
		{"low", ""},
	}
	for _, c := range cases {
		got := colorPriority(c.priority, true)
		want := c.priority
		if c.code != "" {
			want = "\x1b[" + c.code + "m" + c.priority + "\x1b[0m"
		}
		if got != want {
			t.Errorf("colorPriority(%q, true) = %q, want %q", c.priority, got, want)
		}
	}
}

// TestWriteTableColoredColumnStaysAligned is the regression guard for
// a real bug caught before Step 9 shipped: text/tabwriter (writeTable's
// original implementation) measures a cell's raw byte length, so a
// colored cell like colorStatus("done", true) — 13 bytes of escape
// sequences plus text, 4 visible runes — counted as 13 wide and pushed
// every column after it out of alignment. writeTable now measures
// visible width instead; this pins that a colored column and an
// uncolored column in the same table position still line up.
func TestWriteTableColoredColumnStaysAligned(t *testing.T) {
	var buf bytes.Buffer
	if err := writeTable(&buf, []string{"REF", "STATUS", "PRIORITY"}, [][]string{
		{"ABC-1", colorStatus("done", true), "high"},
		{"ABC-2", "backlog", "low"},
	}); err != nil {
		t.Fatalf("writeTable: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("writeTable output has %d lines, want 3 (header + 2 rows): %q", len(lines), buf.String())
	}
	// Strip ANSI escapes before comparing offsets: they add invisible
	// bytes before "high" that would make even correctly visually
	// aligned rows look byte-misaligned if left in.
	visible1 := ansiEscapePattern.ReplaceAllString(lines[1], "")
	col1 := strings.Index(visible1, "high")
	col2 := strings.Index(lines[2], "low")
	if col1 == -1 || col2 == -1 {
		t.Fatalf("expected column values not found: %q", lines)
	}
	if col1 != col2 {
		t.Errorf("PRIORITY column starts at visible offset %d in the colored row but %d in the plain row — columns misaligned:\n%s", col1, col2, buf.String())
	}
}

// TestColorEnabledForcedOff proves colorEnabled never fires for --json
// output or under --no-color/NO_COLOR, regardless of whether stdout
// happens to be a terminal — these two checks must short-circuit
// before stdoutIsTerminal is even consulted.
func TestColorEnabledForcedOff(t *testing.T) {
	if colorEnabled(&clientConfig{JSON: true, NoColor: false}) {
		t.Error("colorEnabled with JSON=true: want false unconditionally")
	}
	if colorEnabled(&clientConfig{JSON: false, NoColor: true}) {
		t.Error("colorEnabled with NoColor=true: want false unconditionally")
	}
	// Under `go test`, os.Stdout is never a terminal, so this is also a
	// real (not vacuous) check that the plain false/false case doesn't
	// crash and resolves through stdoutIsTerminal rather than always
	// returning true.
	if colorEnabled(&clientConfig{JSON: false, NoColor: false}) {
		t.Error("colorEnabled under `go test` (stdout is a pipe, not a terminal): want false")
	}
}

// TestNoColorFlagAndEnvWireIntoClientConfig proves --no-color and
// NO_COLOR both actually set clientConfig.NoColor — the flag was
// previously removed from this codebase for being exactly this kind
// of no-op (registered but nothing read it); this is the regression
// guard.
func TestNoColorFlagAndEnvWireIntoClientConfig(t *testing.T) {
	isolateClientEnv(t)

	fs, cfg, err := newClientFlagSet("test")
	if err != nil {
		t.Fatalf("newClientFlagSet: %v", err)
	}
	if err := fs.Parse([]string{"--no-color"}); err != nil {
		t.Fatalf("parse --no-color: %v", err)
	}
	if !cfg.NoColor {
		t.Error("clientConfig.NoColor after --no-color: want true")
	}

	t.Setenv("NO_COLOR", "1")
	fs2, cfg2, err := newClientFlagSet("test")
	if err != nil {
		t.Fatalf("newClientFlagSet: %v", err)
	}
	if err := fs2.Parse(nil); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !cfg2.NoColor {
		t.Error("clientConfig.NoColor with NO_COLOR=1 set and no flag: want true")
	}
}
