package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

// writeJSON writes v as indented, stable-field-order JSON (struct tag
// order, which encoding/json already preserves) — the CLI's --json
// mode, matching the shape a script parsing the HTTP API's own
// responses already expects (docs/contracts/cli.md).
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// stdoutIsTerminal reports whether os.Stdout looks like a real
// terminal rather than a pipe/redirect/file — the same os.ModeCharDevice
// heuristic docs/contracts/cli.md documents (no new terminal-detection
// dependency, matching this package's existing minimalism).
func stdoutIsTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// colorEnabled reports whether a command's colorStatus/colorPriority
// calls should actually emit ANSI escapes: never in --json mode (a
// script consumer parses that, and escapes would corrupt it if they
// ever leaked into a string value), never with --no-color/NO_COLOR
// set, and never when stdout isn't a terminal (a pipe or redirect) —
// https://no-color.org's convention plus "don't color a file."
func colorEnabled(cfg *clientConfig) bool {
	return !cfg.JSON && !cfg.NoColor && stdoutIsTerminal()
}

// ansiColor wraps s in SGR color code when enabled, otherwise returns
// s unchanged — the single choke point every colored column goes
// through, so a disabled caller sees exactly the same text with zero
// escape sequences, not just "less color."
func ansiColor(code, s string, enabled bool) string {
	if !enabled {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// colorStatus colors a ticket/feature/decision status value: green for
// a settled-good state, red for a settled-bad/blocked one, yellow for
// active work, unchanged otherwise (backlog/ready/proposed and any
// value this table doesn't recognize — new statuses degrade to plain
// text, not a wrong color).
func colorStatus(status string, enabled bool) string {
	switch status {
	case "done", "accepted":
		return ansiColor("32", status, enabled)
	case "blocked", "cancelled", "rejected":
		return ansiColor("31", status, enabled)
	case "in_progress", "review":
		return ansiColor("33", status, enabled)
	default:
		return status
	}
}

// colorPriority colors a critical/high priority or severity value red/
// yellow; medium, low, and any unrecognized value are left plain.
func colorPriority(priority string, enabled bool) string {
	switch priority {
	case "critical":
		return ansiColor("31", priority, enabled)
	case "high":
		return ansiColor("33", priority, enabled)
	default:
		return priority
	}
}

// splitCommaList splits a comma-separated flag value into trimmed,
// non-empty names — the CLI-side counterpart to internal/httpapi's
// fieldNames/includeNames. Returns nil for an empty input, matching
// GetTicketFields/ListTicketsFields' "nil/empty means omit the query
// param" contract.
func splitCommaList(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, f := range strings.Split(s, ",") {
		f = strings.TrimSpace(f)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// writeProjectedRow renders a single ?fields=-projected map as one
// table row, columns in the order fields was given — a server-side
// projection has no fixed column set, so unlike every other table
// helper in this file, the headers here come from the caller's own
// requested field names rather than a hardcoded list.
func writeProjectedRow(w io.Writer, fields []string, row map[string]any) error {
	headers := make([]string, len(fields))
	cells := make([]string, len(fields))
	for i, f := range fields {
		headers[i] = strings.ToUpper(f)
		cells[i] = projectedCellString(row[f])
	}
	return writeTable(w, headers, [][]string{cells})
}

// writeProjectedRows is writeProjectedRow for a list response, plus
// the trailing next_cursor line every paginated table command prints.
func writeProjectedRows(w io.Writer, fields []string, rows []map[string]any, nextCursor string) error {
	headers := make([]string, len(fields))
	for i, f := range fields {
		headers[i] = strings.ToUpper(f)
	}
	tableRows := make([][]string, len(rows))
	for i, row := range rows {
		cells := make([]string, len(fields))
		for j, f := range fields {
			cells[j] = projectedCellString(row[f])
		}
		tableRows[i] = cells
	}
	if err := writeTable(w, headers, tableRows); err != nil {
		return err
	}
	if nextCursor != "" {
		_, err := fmt.Fprintf(w, "next_cursor: %s\n", nextCursor)
		return err
	}
	return nil
}

// projectedCellString renders one field's value from a ?fields=-
// projected map[string]any as a table cell: a missing key (the server
// only includes populated/requested keys) prints as "-", not "<nil>".
func projectedCellString(v any) string {
	if v == nil {
		return "-"
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// ansiEscapePattern matches an SGR color escape ("\x1b[31m", "\x1b[0m")
// — the only kind ansiColor ever emits — so visibleWidth can measure a
// colored cell's on-screen width, not its byte length.
var ansiEscapePattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

// visibleWidth is a cell's on-screen rune width with any ANSI color
// escapes stripped first. text/tabwriter (this file's previous
// implementation) has no concept of this — it measures raw bytes, so a
// colored cell like colorStatus("done", true) counted as 13 runes
// instead of 4 and threw off every column after it. writeTable does
// its own column measurement below specifically to avoid that.
func visibleWidth(s string) int {
	return utf8.RuneCountInString(ansiEscapePattern.ReplaceAllString(s, ""))
}

// padCell right-pads s with spaces until it reaches width, measuring
// by visibleWidth rather than len/utf8.RuneCountInString(s) directly —
// see visibleWidth's doc comment.
func padCell(s string, width int) string {
	if pad := width - visibleWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// writeTable writes a simple padded-column table — the CLI's default,
// human-readable output. No external dependency (matching this
// package's "no CLI framework" convention): column widths are computed
// by hand rather than via text/tabwriter, which measures raw bytes and
// would misalign a colored column (see visibleWidth). The last column
// in each row is never padded — trailing whitespace serves no purpose
// and each row already ends in a newline.
func writeTable(w io.Writer, headers []string, rows [][]string) error {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = visibleWidth(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) {
				if vw := visibleWidth(cell); vw > widths[i] {
					widths[i] = vw
				}
			}
		}
	}

	writeRow := func(cells []string) error {
		padded := make([]string, len(cells))
		for i, c := range cells {
			if i < len(cells)-1 && i < len(widths) {
				padded[i] = padCell(c, widths[i])
			} else {
				padded[i] = c
			}
		}
		_, err := fmt.Fprintln(w, strings.Join(padded, "  "))
		return err
	}

	if err := writeRow(headers); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writeRow(row); err != nil {
			return err
		}
	}
	return nil
}
