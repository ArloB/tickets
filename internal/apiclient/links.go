package apiclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/ArloB/tickets/internal/domain"
)

// linksPathPrefix picks the URL prefix for one of the five link-
// capable kinds (tickets, features, decisions, plans, documents — no
// project case, unlike commentsPathPrefix: product spec §5.11 scopes
// named external links to these five, and internal/httpapi/server.go
// registers addLink/listLinks/removeLink under exactly these five
// route prefixes).
func linksPathPrefix(ref string) (string, error) {
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
		return "", fmt.Errorf("apiclient: external links are not supported for a %q reference", parsed.Kind)
	}
}

// ExternalLink mirrors internal/httpapi/links.go's externalLinkView
// field-for-field.
type ExternalLink struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// AddLinkRequest is POST .../links' request body.
type AddLinkRequest struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// AddLink is POST {ref's kind}/{ref}/links.
func (c *Client) AddLink(ctx context.Context, ref string, req AddLinkRequest) (ExternalLink, error) {
	prefix, err := linksPathPrefix(ref)
	if err != nil {
		return ExternalLink{}, err
	}
	var link ExternalLink
	err = c.do(ctx, http.MethodPost, prefix+url.PathEscape(ref)+"/links", req, &link, requestOptions{})
	return link, err
}

// ListLinks is GET {ref's kind}/{ref}/links.
func (c *Client) ListLinks(ctx context.Context, ref string) ([]ExternalLink, error) {
	prefix, err := linksPathPrefix(ref)
	if err != nil {
		return nil, err
	}
	var out struct {
		Links []ExternalLink `json:"links"`
	}
	err = c.do(ctx, http.MethodGet, prefix+url.PathEscape(ref)+"/links", nil, &out, requestOptions{})
	return out.Links, err
}

// RemoveLink is DELETE {ref's kind}/{ref}/links/{id}.
func (c *Client) RemoveLink(ctx context.Context, ref string, id int64) error {
	prefix, err := linksPathPrefix(ref)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, prefix+url.PathEscape(ref)+"/links/"+strconv.FormatInt(id, 10), nil, nil, requestOptions{})
}
