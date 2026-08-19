package domain

import (
	"reflect"
	"testing"
)

// TestScanReferences is the Phase 1 plan's verification gate 7: a body
// containing #ABC-2, bare ABC-3, project-scoped #4, and ABC-5 inside a
// code fence must produce exactly three references, not four.
func TestScanReferences(t *testing.T) {
	text := "See #ABC-2 and ABC-3, also #4 for context.\n\n```\nExample: ABC-5\n```\n"
	got := ScanReferences(text, "ABC")
	want := []Reference{
		{ProjectKey: "ABC", Kind: KindTicket, Seq: 2},
		{ProjectKey: "ABC", Kind: KindTicket, Seq: 3},
		{ProjectKey: "ABC", Kind: KindTicket, Seq: 4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ScanReferences() = %+v, want %+v", got, want)
	}
}

func TestScanReferencesTableDriven(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		scope string
		want  []Reference
	}{
		{
			name: "bare ticket",
			text: "Fixed in ABC-123.",
			want: []Reference{{"ABC", KindTicket, 123}},
		},
		{
			name: "hash prefixed",
			text: "See #ABC-123.",
			want: []Reference{{"ABC", KindTicket, 123}},
		},
		{
			name: "all five kinds",
			text: "ABC-123 ABC-F12 ABC-D7 ABC-P4 ABC-DOC9",
			want: []Reference{
				{"ABC", KindTicket, 123},
				{"ABC", KindFeature, 12},
				{"ABC", KindDecision, 7},
				{"ABC", KindPlan, 4},
				{"ABC", KindDocument, 9},
			},
		},
		{
			name:  "short form requires scope",
			text:  "See #42.",
			scope: "",
			want:  nil,
		},
		{
			name:  "short form with scope",
			text:  "See #42.",
			scope: "ABC",
			want:  []Reference{{"ABC", KindTicket, 42}},
		},
		{
			name: "fenced code block excluded",
			text: "before\n```\nABC-1\n```\nafter",
			want: nil,
		},
		{
			name: "tilde fenced code block excluded",
			text: "before\n~~~\nABC-1\n~~~\nafter",
			want: nil,
		},
		{
			name: "inline code span excluded",
			text: "see `ABC-1` here",
			want: nil,
		},
		{
			name: "not a suffix of a longer word",
			text: "fooABC-123 stays unmatched, but ABC-123 alone matches",
			want: []Reference{{"ABC", KindTicket, 123}},
		},
		{
			name: "not a prefix of a longer token",
			text: "ABC-123abc should not match",
			want: nil,
		},
		{
			name: "duplicate mentions deduplicated",
			text: "ABC-1 appears twice: ABC-1.",
			want: []Reference{{"ABC", KindTicket, 1}},
		},
		{
			name: "unknown kind letter skipped",
			text: "ABC-X123 is not a real reference",
			want: nil,
		},
		{
			name: "leading zero sequence skipped",
			text: "ABC-007 is not a valid sequence",
			want: nil,
		},
		{
			name:  "short form trailing word char excluded",
			text:  "call #123abc not a ticket",
			scope: "ABC",
			want:  nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanReferences(tc.text, tc.scope)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ScanReferences(%q, %q) = %+v, want %+v", tc.text, tc.scope, got, tc.want)
			}
		})
	}
}
