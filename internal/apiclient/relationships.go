package apiclient

import (
	"context"
	"net/http"
	"net/url"
)

// RelationshipView is one relationship edge as seen from the ticket
// the caller asked about, mirroring internal/httpapi/wire.go's
// relationshipView.
type RelationshipView struct {
	Type  string `json:"type"`
	Other string `json:"other"`
}

// RelationshipsPage is GET /tickets/{ref}/relationships' response
// envelope.
type RelationshipsPage struct {
	Relationships []RelationshipView `json:"relationships"`
}

// AddRelationship is POST /tickets/{ref}/relationships. relType is one
// of the 8 wire values (product spec §5.7: parent_of, child_of,
// blocks, blocked_by, related_to, duplicate_of, supersedes,
// superseded_by) — relationships are ticket-to-ticket only, unlike
// associations.
func (c *Client) AddRelationship(ctx context.Context, sourceRef, relType, targetRef string) error {
	return c.do(ctx, http.MethodPost, "/tickets/"+url.PathEscape(sourceRef)+"/relationships",
		struct {
			Target string `json:"target"`
			Type   string `json:"type"`
		}{Target: targetRef, Type: relType},
		nil, requestOptions{})
}

// ListRelationships is GET /tickets/{ref}/relationships.
func (c *Client) ListRelationships(ctx context.Context, sourceRef string) (RelationshipsPage, error) {
	var page RelationshipsPage
	err := c.do(ctx, http.MethodGet, "/tickets/"+url.PathEscape(sourceRef)+"/relationships", nil, &page, requestOptions{})
	return page, err
}

// RemoveRelationship is DELETE /tickets/{ref}/relationships/{type}/{target}.
func (c *Client) RemoveRelationship(ctx context.Context, sourceRef, relType, targetRef string) error {
	return c.do(ctx, http.MethodDelete,
		"/tickets/"+url.PathEscape(sourceRef)+"/relationships/"+url.PathEscape(relType)+"/"+url.PathEscape(targetRef),
		nil, nil, requestOptions{})
}
