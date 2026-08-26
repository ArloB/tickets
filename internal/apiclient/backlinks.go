package apiclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/ArloB/tickets/internal/domain"
)

// backlinksPathPrefix picks the URL prefix for one of the five
// backlink-capable kinds, mirroring linksPathPrefix's dispatch (the
// same five kinds internal/httpapi/server.go registers listBacklinks
// under).
func backlinksPathPrefix(ref string) (string, error) {
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
		return "", fmt.Errorf("apiclient: backlinks are not supported for a %q reference", parsed.Kind)
	}
}

// Backlink mirrors internal/httpapi/backlinks.go's backlinkView.
type Backlink struct {
	Ref       string `json:"ref"`
	CommentID *int64 `json:"comment_id,omitempty"`
}

// ListBacklinks is GET {ref's kind}/{ref}/backlinks.
func (c *Client) ListBacklinks(ctx context.Context, ref string) ([]Backlink, error) {
	prefix, err := backlinksPathPrefix(ref)
	if err != nil {
		return nil, err
	}
	var out struct {
		Backlinks []Backlink `json:"backlinks"`
	}
	err = c.do(ctx, http.MethodGet, prefix+url.PathEscape(ref)+"/backlinks", nil, &out, requestOptions{})
	return out.Backlinks, err
}
