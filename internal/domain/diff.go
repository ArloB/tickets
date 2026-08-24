package domain

import "strings"

// DiffOp identifies one DiffLine's role in a line-level diff.
type DiffOp string

const (
	DiffEqual  DiffOp = "equal"
	DiffAdd    DiffOp = "add"
	DiffRemove DiffOp = "remove"
)

// DiffLine is one line of a computed line-level diff (§5.9: "the UI
// computes a line-level diff between versions").
type DiffLine struct {
	Op   DiffOp `json:"op"`
	Text string `json:"text"`
}

// LineDiff computes a minimal line-level diff between oldText and
// newText using the standard LCS-based algorithm: an O(n*m) dynamic
// program over the two line sequences, backtracked to produce an
// ordered add/remove/equal sequence. Pure function, no I/O — shared by
// decision version diffs (Phase 5 Step 2) and, later, content_item
// (plan/document) Markdown version diffs (Step 3), so it lives in
// internal/domain rather than internal/service.
//
// O(n*m) time and space is fine for the short prose fields this diffs
// (decision context/decision/rationale/consequences, plan/document
// Markdown bodies) — this is not meant for large file diffing.
func LineDiff(oldText, newText string) []DiffLine {
	a := splitLines(oldText)
	b := splitLines(newText)
	n, m := len(a), len(b)

	// dp[i][j] = length of the longest common subsequence of a[i:] and
	// b[j:], filled bottom-up so backtracking from (0,0) can walk
	// forward through both sequences in original order.
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	var out []DiffLine
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, DiffLine{Op: DiffEqual, Text: a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, DiffLine{Op: DiffRemove, Text: a[i]})
			i++
		default:
			out = append(out, DiffLine{Op: DiffAdd, Text: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, DiffLine{Op: DiffRemove, Text: a[i]})
	}
	for ; j < m; j++ {
		out = append(out, DiffLine{Op: DiffAdd, Text: b[j]})
	}
	return out
}

// splitLines splits s on "\n" the way LineDiff wants: "" is zero
// lines, not strings.Split's own [""] — otherwise diffing two empty
// fields would produce a spurious single equal empty-string line, and
// diffing "" against "hello" would show a remove of "" rather than a
// clean single add.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
