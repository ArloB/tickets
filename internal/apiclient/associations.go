package apiclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/ArloB/tickets/internal/domain"
)

// AssociationsPage is GET .../associations' response envelope,
// mirroring internal/httpapi/wire.go's associationsPage.
type AssociationsPage struct {
	Associated []string `json:"associated"`
}

// associationsPathPrefix picks /tickets/, /features/, or /decisions/
// based on ref's own kind — internal/httpapi mounts the identical
// association routes under all three (product spec §5.7:
// "associated_with" spans ticket, feature, and decision). A ref of any
// other kind is rejected here, client-side, rather than reaching the
// server as a request against a route that doesn't exist.
func associationsPathPrefix(ref string) (string, error) {
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
	default:
		return "", fmt.Errorf("apiclient: associations are not supported for a %q reference", parsed.Kind)
	}
}

// AddAssociation is POST /tickets/{ref}/associations or
// POST /features/{ref}/associations, chosen by ref's own kind.
func (c *Client) AddAssociation(ctx context.Context, ref, target string) error {
	prefix, err := associationsPathPrefix(ref)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, prefix+url.PathEscape(ref)+"/associations",
		struct {
			Target string `json:"target"`
		}{Target: target},
		nil, requestOptions{})
}

// ListAssociations is GET .../associations.
func (c *Client) ListAssociations(ctx context.Context, ref string) (AssociationsPage, error) {
	prefix, err := associationsPathPrefix(ref)
	if err != nil {
		return AssociationsPage{}, err
	}
	var page AssociationsPage
	err = c.do(ctx, http.MethodGet, prefix+url.PathEscape(ref)+"/associations", nil, &page, requestOptions{})
	return page, err
}

// RemoveAssociation is DELETE .../associations/{target}.
func (c *Client) RemoveAssociation(ctx context.Context, ref, target string) error {
	prefix, err := associationsPathPrefix(ref)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, prefix+url.PathEscape(ref)+"/associations/"+url.PathEscape(target), nil, nil, requestOptions{})
}
