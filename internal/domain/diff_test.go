package domain

import (
	"reflect"
	"testing"
)

func TestLineDiffIdenticalText(t *testing.T) {
	got := LineDiff("a\nb\nc", "a\nb\nc")
	want := []DiffLine{
		{Op: DiffEqual, Text: "a"},
		{Op: DiffEqual, Text: "b"},
		{Op: DiffEqual, Text: "c"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LineDiff(identical) = %+v, want %+v", got, want)
	}
}

func TestLineDiffPureAddition(t *testing.T) {
	got := LineDiff("a\nb", "a\nb\nc")
	want := []DiffLine{
		{Op: DiffEqual, Text: "a"},
		{Op: DiffEqual, Text: "b"},
		{Op: DiffAdd, Text: "c"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LineDiff(pure addition) = %+v, want %+v", got, want)
	}
}

func TestLineDiffPureRemoval(t *testing.T) {
	got := LineDiff("a\nb\nc", "a\nb")
	want := []DiffLine{
		{Op: DiffEqual, Text: "a"},
		{Op: DiffEqual, Text: "b"},
		{Op: DiffRemove, Text: "c"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LineDiff(pure removal) = %+v, want %+v", got, want)
	}
}

func TestLineDiffMiddleReplacement(t *testing.T) {
	got := LineDiff("a\nb\nc", "a\nx\nc")
	want := []DiffLine{
		{Op: DiffEqual, Text: "a"},
		{Op: DiffRemove, Text: "b"},
		{Op: DiffAdd, Text: "x"},
		{Op: DiffEqual, Text: "c"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LineDiff(middle replacement) = %+v, want %+v", got, want)
	}
}

func TestLineDiffEmptyToNonEmpty(t *testing.T) {
	got := LineDiff("", "hello")
	want := []DiffLine{{Op: DiffAdd, Text: "hello"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LineDiff(\"\", \"hello\") = %+v, want %+v", got, want)
	}
}

func TestLineDiffNonEmptyToEmpty(t *testing.T) {
	got := LineDiff("hello", "")
	want := []DiffLine{{Op: DiffRemove, Text: "hello"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LineDiff(\"hello\", \"\") = %+v, want %+v", got, want)
	}
}

func TestLineDiffBothEmpty(t *testing.T) {
	got := LineDiff("", "")
	if len(got) != 0 {
		t.Errorf("LineDiff(\"\", \"\") = %+v, want empty", got)
	}
}

// TestLineDiffReconstructsOldAndNew is a property test: filtering the
// diff to non-add lines reconstructs oldText, and filtering to
// non-remove lines reconstructs newText — true of any correct diff
// regardless of the specific edit script chosen.
func TestLineDiffReconstructsOldAndNew(t *testing.T) {
	cases := [][2]string{
		{"a\nb\nc\nd", "b\nc\nd\ne"},
		{"one line", "two\nlines"},
		{"", ""},
		{"same", "same"},
		{"x\ny\nz", ""},
	}
	for _, c := range cases {
		oldText, newText := c[0], c[1]
		diff := LineDiff(oldText, newText)

		var reconstructedOld, reconstructedNew []string
		for _, line := range diff {
			if line.Op != DiffAdd {
				reconstructedOld = append(reconstructedOld, line.Text)
			}
			if line.Op != DiffRemove {
				reconstructedNew = append(reconstructedNew, line.Text)
			}
		}
		if got := joinOrEmpty(reconstructedOld); got != oldText {
			t.Errorf("LineDiff(%q, %q): reconstructed old = %q, want %q", oldText, newText, got, oldText)
		}
		if got := joinOrEmpty(reconstructedNew); got != newText {
			t.Errorf("LineDiff(%q, %q): reconstructed new = %q, want %q", oldText, newText, got, newText)
		}
	}
}

func joinOrEmpty(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
