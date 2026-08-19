package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// EntityKind identifies an entity's kind across the whole system: the
// public reference grammar (docs/contracts/references.md),
// reference_counters.kind (ADR 0009), and entities.kind (ADR 0002) are
// all instances of this one type, not three parallel string
// vocabularies that happen to agree by convention. Before this type
// existed, internal/service used ad-hoc string literals ("project",
// "ticket", "feature") that had to be kept in sync with this package's
// ReferenceKind by hand; EntityKind removes that duplication.
//
// KindProject is a deliberate exception to the reference grammar
// below: a project is named by its Key, not a sequence-numbered
// reference token, so it has no entry in kindCode and Format rejects
// it explicitly (see Format).
type EntityKind string

const (
	KindProject  EntityKind = "project"
	KindTicket   EntityKind = "ticket"
	KindFeature  EntityKind = "feature"
	KindDecision EntityKind = "decision"
	KindPlan     EntityKind = "plan"
	KindDocument EntityKind = "document"
)

var validEntityKinds = map[EntityKind]bool{
	KindProject:  true,
	KindTicket:   true,
	KindFeature:  true,
	KindDecision: true,
	KindPlan:     true,
	KindDocument: true,
}

// Valid reports whether k is a recognized entity kind, including
// KindProject even though it has no reference-token form.
func (k EntityKind) Valid() bool {
	return validEntityKinds[k]
}

// kindCode maps a referenceable EntityKind to its letter code in the
// reference token grammar. Ticket has no letter code by design
// (docs/contracts/references.md): "ABC-123", not "ABC-T123". Deliberately
// has no entry for KindProject — see the type doc.
var kindCode = map[EntityKind]string{
	KindTicket:   "",
	KindFeature:  "F",
	KindDecision: "D",
	KindPlan:     "P",
	KindDocument: "DOC",
}

var codeKind = map[string]EntityKind{
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
	Kind       EntityKind
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
	if ref.Kind == KindProject {
		return "", fmt.Errorf("domain: a project has no reference token; use its key directly")
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
