package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ReferenceKind identifies which entity kind a public reference names.
// See docs/contracts/references.md for the wire grammar this file
// implements.
type ReferenceKind string

const (
	KindTicket   ReferenceKind = "ticket"
	KindFeature  ReferenceKind = "feature"
	KindDecision ReferenceKind = "decision"
	KindPlan     ReferenceKind = "plan"
	KindDocument ReferenceKind = "document"
)

// kindCode maps a ReferenceKind to its letter code in the reference
// token grammar. Ticket has no letter code by design (docs/contracts/
// references.md): "ABC-123", not "ABC-T123".
var kindCode = map[ReferenceKind]string{
	KindTicket:   "",
	KindFeature:  "F",
	KindDecision: "D",
	KindPlan:     "P",
	KindDocument: "DOC",
}

var codeKind = map[string]ReferenceKind{
	"":    KindTicket,
	"F":   KindFeature,
	"D":   KindDecision,
	"P":   KindPlan,
	"DOC": KindDocument,
}

var projectKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

// referencePattern captures (project key)(kind code, may be empty)(seq).
// Kind codes are tried longest-first (DOC before D) in the alternation
// so "ABC-DOC9" doesn't spuriously match the "D" branch against "DOC9".
var referencePattern = regexp.MustCompile(`^([A-Z][A-Z0-9]{1,9})-(DOC|F|D|P)?([1-9][0-9]*)$`)

// Reference is a parsed public reference, e.g. "ABC-F12" ->
// Reference{ProjectKey: "ABC", Kind: KindFeature, Seq: 12}.
type Reference struct {
	ProjectKey string
	Kind       ReferenceKind
	Seq        int64
}

// ValidProjectKey reports whether key matches the project-key grammar:
// uppercase letters and digits, 2-10 characters, starting with a
// letter.
func ValidProjectKey(key string) bool {
	return projectKeyPattern.MatchString(key)
}

// Format renders a Reference as its wire token. It is the inverse of
// Parse for every value Parse can produce.
func Format(ref Reference) (string, error) {
	if !ValidProjectKey(ref.ProjectKey) {
		return "", fmt.Errorf("domain: invalid project key %q", ref.ProjectKey)
	}
	code, ok := kindCode[ref.Kind]
	if !ok {
		return "", fmt.Errorf("domain: unknown reference kind %q", ref.Kind)
	}
	if ref.Seq <= 0 {
		return "", fmt.Errorf("domain: reference sequence must be positive, got %d", ref.Seq)
	}
	return fmt.Sprintf("%s-%s%d", ref.ProjectKey, code, ref.Seq), nil
}

// Parse accepts both the bare form (ABC-123) and the '#'-prefixed form
// (#ABC-123) recognized in Markdown fields (docs/contracts/references.md).
// It rejects lowercase input, leading zeros in the sequence, and
// unknown kind codes rather than guessing.
func Parse(s string) (Reference, error) {
	trimmed := strings.TrimPrefix(s, "#")
	m := referencePattern.FindStringSubmatch(trimmed)
	if m == nil {
		return Reference{}, fmt.Errorf("domain: %q is not a valid reference", s)
	}
	key, code, seqStr := m[1], m[2], m[3]
	kind, ok := codeKind[code]
	if !ok {
		return Reference{}, fmt.Errorf("domain: %q has unknown kind code %q", s, code)
	}
	seq, err := strconv.ParseInt(seqStr, 10, 64)
	if err != nil {
		return Reference{}, fmt.Errorf("domain: %q has invalid sequence: %w", s, err)
	}
	return Reference{ProjectKey: key, Kind: kind, Seq: seq}, nil
}
