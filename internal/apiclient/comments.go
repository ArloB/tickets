package apiclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ArloB/tickets/internal/domain"
)

// commentsPathPrefix picks the URL prefix for one of the six
// commentable kinds §5.10 names (Phase 6 Step 2 — previously
// ticket-only), mirroring associationsPathPrefix's dispatch pattern.
// A project ref has no seq-numbered token domain.Parse recognizes (see
// domain.Parse's doc), so a bare project key is checked first via
// domain.ValidProjectKey before falling through to domain.Parse for
// the other five kinds.
func commentsPathPrefix(ref string) (string, error) {
	if domain.ValidProjectKey(ref) {
		return "/projects/", nil
	}
	parsed, err := domain.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("apiclient: parse reference %q: %w", ref, err)
	}
	switch parsed.Kind {
	case domain.KindTicket:
		return "/tickets/", nil
	case domain.KindFeature:
		return "/features/", nil
	case domain.KindDecision:
		return "/decisions/", nil
	case domain.KindPlan:
		return "/plans/", nil
	case domain.KindDocument:
		return "/documents/", nil
	default:
		return "", fmt.Errorf("apiclient: comments are not supported for a %q reference", parsed.Kind)
	}
}

// Comment mirrors internal/httpapi/wire.go's commentDetail
// field-for-field.
type Comment struct {
	ID        int64      `json:"id"`
	Author    string     `json:"author"`
	Body      string     `json:"body"`
	Version   int64      `json:"version"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// CommentVersion mirrors internal/httpapi/wire.go's commentVersionEntry.
type CommentVersion struct {
	Version   int64     `json:"version"`
	Body      string    `json:"body"`
	EditedBy  string    `json:"edited_by"`
	CreatedAt time.Time `json:"created_at"`
}

// CommentsPage is GET /tickets/{ref}/comments' response envelope —
// unpaginated on the wire today (internal/httpapi/wire.go's
// commentsPage has no next_cursor), matching that exactly.
type CommentsPage struct {
	Comments []Comment `json:"comments"`
}

// CommentHistoryPage is GET /comments/{id}/history's response envelope.
type CommentHistoryPage struct {
	Versions []CommentVersion `json:"versions"`
}

// CreateComment is POST /tickets/{ref}/comments, or the matching
// route for any of the other five commentable kinds §5.10 names,
// chosen by ref's own kind (commentsPathPrefix).
func (c *Client) CreateComment(ctx context.Context, ref, body, idempotencyKey string) (Comment, error) {
	prefix, err := commentsPathPrefix(ref)
	if err != nil {
		return Comment{}, err
	}
	var comment Comment
	err = c.do(ctx, http.MethodPost, prefix+url.PathEscape(ref)+"/comments",
		struct {
			Body string `json:"body"`
		}{Body: body},
		&comment, requestOptions{IdempotencyKey: idempotencyKey})
	return comment, err
}

// ListComments is GET .../comments, chosen by ref's own kind.
func (c *Client) ListComments(ctx context.Context, ref string) (CommentsPage, error) {
	prefix, err := commentsPathPrefix(ref)
	if err != nil {
		return CommentsPage{}, err
	}
	var page CommentsPage
	err = c.do(ctx, http.MethodGet, prefix+url.PathEscape(ref)+"/comments", nil, &page, requestOptions{})
	return page, err
}

// GetComment is GET /comments/{id}.
func (c *Client) GetComment(ctx context.Context, id int64) (Comment, error) {
	var comment Comment
	err := c.do(ctx, http.MethodGet, "/comments/"+strconv.FormatInt(id, 10), nil, &comment, requestOptions{})
	return comment, err
}

// GetCommentHistory is GET /comments/{id}/history.
func (c *Client) GetCommentHistory(ctx context.Context, id int64) (CommentHistoryPage, error) {
	var page CommentHistoryPage
	err := c.do(ctx, http.MethodGet, "/comments/"+strconv.FormatInt(id, 10)+"/history", nil, &page, requestOptions{})
	return page, err
}

// EditComment is PATCH /comments/{id}. A comment's version is its own
// (comments.version), independent of its parent ticket's — see
// docs/contracts/concurrency.md's "Phase 1 addendum."
func (c *Client) EditComment(ctx context.Context, id, expectedVersion int64, body string) (Comment, error) {
	var comment Comment
	err := c.do(ctx, http.MethodPatch, "/comments/"+strconv.FormatInt(id, 10),
		struct {
			Body string `json:"body"`
		}{Body: body},
		&comment, requestOptions{IfMatch: &expectedVersion})
	return comment, err
}

// DeleteComment is DELETE /comments/{id} (soft delete — the tombstone
// stays visible, product spec §5.10). The server's response body is
// just {"status":"deleted"}, nothing a caller needs back.
func (c *Client) DeleteComment(ctx context.Context, id, expectedVersion int64) error {
	return c.do(ctx, http.MethodDelete, "/comments/"+strconv.FormatInt(id, 10), nil, nil, requestOptions{IfMatch: &expectedVersion})
}
