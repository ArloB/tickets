package mcpsrv

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateActivityExcerptTruncatesByRune(t *testing.T) {
	body := strings.Repeat("é", 250)
	excerpt := truncateActivityExcerpt(body)

	if !utf8.ValidString(excerpt) {
		t.Fatalf("excerpt is not valid UTF-8: %q", excerpt)
	}
	if !strings.HasSuffix(excerpt, "…") {
		t.Errorf("excerpt = %q, want it to end with an ellipsis marking the truncation", excerpt)
	}
	if got := utf8.RuneCountInString(strings.TrimSuffix(excerpt, "…")); got != activityCommentExcerptLimit {
		t.Errorf("excerpt rune count (excluding the ellipsis) = %d, want %d", got, activityCommentExcerptLimit)
	}
}

func TestTruncateActivityExcerptTruncatesAtWordBoundary(t *testing.T) {
	body := strings.Repeat("abcde ", 40)
	excerpt := truncateActivityExcerpt(body)

	if !strings.HasSuffix(excerpt, "…") {
		t.Fatalf("excerpt = %q, want it to end with an ellipsis", excerpt)
	}
	trimmed := strings.TrimSuffix(excerpt, "…")
	if strings.HasSuffix(trimmed, " ") {
		t.Errorf("excerpt = %q, want no trailing space before the ellipsis", excerpt)
	}
	for _, word := range strings.Fields(trimmed) {
		if word != "abcde" {
			t.Errorf("excerpt = %q, contains a partial word %q — truncation cut mid-word", excerpt, word)
		}
	}
}

func TestTruncateActivityExcerptLeavesShortBodyUntouched(t *testing.T) {
	body := "short comment"
	if got := truncateActivityExcerpt(body); got != body {
		t.Errorf("truncateActivityExcerpt(%q) = %q, want it unchanged", body, got)
	}
}
