package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ArloB/tickets/internal/service"
)

// TestActivityFeedOverHTTP is Phase 5 Step 1's route-wiring exit check:
// creating a project, a ticket, and commenting on it all show up,
// newest first, validated against api/openapi.yaml by ts.do.
func TestActivityFeedOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]any{"type": "task", "title": "T", "general": true}))
	ts.do(http.MethodPost, "/tickets/ABC-1/comments", nil, mustJSON(t, map[string]string{"body": "First comment"}))

	resp, body := ts.do(http.MethodGet, "/projects/ABC/activity", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list activity status = %d, body=%s", resp.StatusCode, body)
	}
	var page struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("unmarshal activity page: %v", err)
	}
	if len(page.Events) != 3 {
		t.Fatalf("events = %+v, want 3 (project_created, ticket_created, comment_added)", page.Events)
	}
	if page.Events[0]["event_type"] != "comment_added" {
		t.Errorf("Events[0].event_type = %v, want comment_added (newest first)", page.Events[0]["event_type"])
	}
	if page.Events[0]["comment_excerpt"] != "First comment" {
		t.Errorf("Events[0].comment_excerpt = %v, want %q", page.Events[0]["comment_excerpt"], "First comment")
	}
	if page.Events[0]["entity"] != "ABC-1" {
		t.Errorf("Events[0].entity = %v, want ABC-1", page.Events[0]["entity"])
	}
	if page.Events[1]["event_type"] != "ticket_created" {
		t.Errorf("Events[1].event_type = %v, want ticket_created", page.Events[1]["event_type"])
	}
	if page.Events[2]["event_type"] != "project_created" {
		t.Errorf("Events[2].event_type = %v, want project_created", page.Events[2]["event_type"])
	}
	if _, ok := page.Events[2]["entity"]; ok {
		t.Errorf("Events[2] (project_created) = %+v, want no entity field (a project has no seq-numbered reference token)", page.Events[2])
	}
}

// TestToActivityEventExcerptTruncatesByRune proves the comment excerpt
// truncates on a rune boundary, not a byte boundary — a byte-offset cut
// through a multi-byte UTF-8 character would corrupt the tail of the
// excerpt (and, via json.Marshal, surface as U+FFFD replacement
// characters in the wire response). The fixture is one unbroken run
// with no whitespace at all, so there's no word boundary to back up
// to — the excerpt is the limit's worth of runes, plus the ellipsis
// that always marks a truncation.
func TestToActivityEventExcerptTruncatesByRune(t *testing.T) {
	body := strings.Repeat("é", 250) // 250 runes, 2 bytes each = 500 bytes
	out := toActivityEvent(service.ActivityEvent{CommentID: ptr(int64(1)), CommentBody: &body})

	if out.CommentExcerpt == nil {
		t.Fatal("CommentExcerpt = nil, want a truncated excerpt")
	}
	excerpt := *out.CommentExcerpt
	if !utf8.ValidString(excerpt) {
		t.Fatalf("excerpt is not valid UTF-8: %q", excerpt)
	}
	if !strings.HasSuffix(excerpt, "…") {
		t.Errorf("excerpt = %q, want it to end with an ellipsis marking the truncation", excerpt)
	}
	if got := utf8.RuneCountInString(strings.TrimSuffix(excerpt, "…")); got != activityCommentExcerptLimit {
		t.Errorf("excerpt rune count (excluding the ellipsis) = %d, want %d", got, activityCommentExcerptLimit)
	}
	if strings.Contains(excerpt, "�") {
		t.Errorf("excerpt contains a replacement character (a byte-boundary cut through a multi-byte rune): %q", excerpt)
	}
}

// TestToActivityEventExcerptTruncatesAtWordBoundary proves a truncated
// excerpt never ends mid-word: the fixture's 200th rune falls inside a
// word ("abcde"), so a naive hard cut would produce a fragment like
// "...abcd" — the excerpt must instead back up to the last complete
// word and mark the cut with an ellipsis.
func TestToActivityEventExcerptTruncatesAtWordBoundary(t *testing.T) {
	body := strings.Repeat("abcde ", 40) // 240 runes; rune 200 lands mid-word
	out := toActivityEvent(service.ActivityEvent{CommentID: ptr(int64(1)), CommentBody: &body})

	if out.CommentExcerpt == nil {
		t.Fatal("CommentExcerpt = nil, want a truncated excerpt")
	}
	excerpt := *out.CommentExcerpt
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

func ptr[T any](v T) *T { return &v }

// TestActivityFeedFiltersByEventType proves the ?event_type= query
// parameter actually narrows results over HTTP, not just at the
// service layer.
func TestActivityFeedFiltersByEventType(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]any{"type": "task", "title": "T", "general": true}))
	ts.do(http.MethodPost, "/tickets/ABC-1/comments", nil, mustJSON(t, map[string]string{"body": "Hi"}))

	resp, body := ts.do(http.MethodGet, "/projects/ABC/activity?event_type=ticket_created", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list activity status = %d, body=%s", resp.StatusCode, body)
	}
	var page struct {
		Events []map[string]any `json:"events"`
	}
	_ = json.Unmarshal(body, &page)
	if len(page.Events) != 1 || page.Events[0]["event_type"] != "ticket_created" {
		t.Errorf("filtered events = %+v, want exactly one ticket_created event", page.Events)
	}
}
