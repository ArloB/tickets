package domain

import (
	"regexp"
	"strconv"
	"strings"
)

// scanPattern finds the bare/#-prefixed 5-kind reference form (see
// Parse) anywhere in free text, not just as the whole string. Kind
// codes are tried longest-first (DOC before D), same reasoning as
// referencePattern: "ABC-DOC9" must not spuriously match the D branch
// against "DOC9".
var scanPattern = regexp.MustCompile(`#?([A-Z][A-Z0-9]{1,9})-(DOC|F|D|P)?([1-9][0-9]*)`)

// shortFormPattern finds the project-scoped, ticket-only short form
// (#123 -> that project's ticket 123, docs/contracts/references.md).
// It never overlaps with scanPattern: the short form has no letters
// between '#' and the digits, so wherever scanPattern's key group
// would need to start, this pattern's digits already do.
var shortFormPattern = regexp.MustCompile(`#([1-9][0-9]*)`)

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// isBoundaryOK rejects a match that's actually the tail of a longer
// identifier (e.g. the "ABC-123" inside "XABC-123") by checking the
// bytes immediately outside the match. A match that begins with '#' is
// exempt from the leading check: '#' is itself a non-word delimiter in
// the overwhelming majority of Markdown text, and references.md
// doesn't define behavior for a reference embedded directly in a
// larger word before the '#'.
func isBoundaryOK(text string, start, end int) bool {
	if start > 0 && text[start] != '#' && isWordByte(text[start-1]) {
		return false
	}
	if end < len(text) && isWordByte(text[end]) {
		return false
	}
	return true
}

// stripCodeRegions blanks fenced code blocks (``` or ~~~ delimited)
// and inline code spans (`...`) to the same line/character shape
// (spaces and blank lines, not removed) so byte offsets inside the
// result still line up with the input for anyone who wants them later,
// even though ScanReferences itself doesn't use offsets. A reference
// written inside a code sample is not a mention (§5.2 talks about
// references in prose; a code block quoting "ABC-123" as example
// output is not the author linking to that ticket).
func stripCodeRegions(text string) string {
	lines := strings.Split(text, "\n")
	inFence := false
	fenceMarker := ""
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if inFence {
			if strings.HasPrefix(trimmed, fenceMarker) {
				inFence = false
			}
			lines[i] = ""
			continue
		}
		if fp := fencePrefix(trimmed); fp != "" {
			inFence = true
			fenceMarker = fp
			lines[i] = ""
			continue
		}
		lines[i] = stripInlineCode(line)
	}
	return strings.Join(lines, "\n")
}

func fencePrefix(trimmed string) string {
	switch {
	case strings.HasPrefix(trimmed, "```"):
		return "```"
	case strings.HasPrefix(trimmed, "~~~"):
		return "~~~"
	default:
		return ""
	}
}

var inlineCodePattern = regexp.MustCompile("`[^`\n]*`")

func stripInlineCode(line string) string {
	return inlineCodePattern.ReplaceAllStringFunc(line, func(m string) string {
		return strings.Repeat(" ", len(m))
	})
}

// ScanReferences finds every reference to another entity inside free
// Markdown text: the bare and '#'-prefixed 5-kind form (Parse's
// grammar) plus, when scopeProjectKey is a valid project key, the
// project-scoped ticket-only short form ('#123'). Fenced code blocks
// and inline code spans are excluded first. Malformed or unresolvable
// candidates (an unrecognized kind letter, a short form with no valid
// scope) are silently skipped rather than reported as errors — this is
// a best-effort scan over prose, unlike the strict single-token Parse.
// Results are deduplicated and returned in first-occurrence order;
// internal/service is what turns them into derived_mentions rows
// (ADR 0015).
func ScanReferences(text, scopeProjectKey string) []Reference {
	clean := stripCodeRegions(text)

	seen := make(map[Reference]bool)
	var refs []Reference
	add := func(r Reference) {
		if !seen[r] {
			seen[r] = true
			refs = append(refs, r)
		}
	}

	for _, m := range scanPattern.FindAllStringSubmatchIndex(clean, -1) {
		start, end := m[0], m[1]
		if !isBoundaryOK(clean, start, end) {
			continue
		}
		key := clean[m[2]:m[3]]
		code := ""
		if m[4] != -1 {
			code = clean[m[4]:m[5]]
		}
		kind, ok := codeKind[code]
		if !ok {
			continue
		}
		seq, err := strconv.ParseInt(clean[m[6]:m[7]], 10, 64)
		if err != nil {
			continue
		}
		add(Reference{ProjectKey: key, Kind: kind, Seq: seq})
	}

	if scopeProjectKey != "" && ValidProjectKey(scopeProjectKey) {
		for _, m := range shortFormPattern.FindAllStringSubmatchIndex(clean, -1) {
			start, end := m[0], m[1]
			if !isBoundaryOK(clean, start, end) {
				continue
			}
			seq, err := strconv.ParseInt(clean[m[2]:m[3]], 10, 64)
			if err != nil {
				continue
			}
			add(Reference{ProjectKey: scopeProjectKey, Kind: KindTicket, Seq: seq})
		}
	}

	return refs
}
