package apiclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/ArloB/tickets/internal/domain"
)

// Subscription mirrors internal/httpapi/subscriptions.go's
// subscriptionView.
type Subscription struct {
	Subscribed bool `json:"subscribed"`
}

// subscriptionPathPrefix picks the right path prefix from ref's own
// kind — every principal kind is subscribable (product spec §6.4),
// unlike associations' three-kind restriction (associations.go).
func subscriptionPathPrefix(ref string) (string, error) {
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
		return "", fmt.Errorf("apiclient: subscriptions are not supported for a %q reference", parsed.Kind)
	}
}

// Subscribe is POST .../subscribe.
func (c *Client) Subscribe(ctx context.Context, ref string) (Subscription, error) {
	prefix, err := subscriptionPathPrefix(ref)
	if err != nil {
		return Subscription{}, err
	}
	var sub Subscription
	err = c.do(ctx, http.MethodPost, prefix+url.PathEscape(ref)+"/subscribe", nil, &sub, requestOptions{})
	return sub, err
}

// Unsubscribe is DELETE .../subscribe.
func (c *Client) Unsubscribe(ctx context.Context, ref string) (Subscription, error) {
	prefix, err := subscriptionPathPrefix(ref)
	if err != nil {
		return Subscription{}, err
	}
	var sub Subscription
	err = c.do(ctx, http.MethodDelete, prefix+url.PathEscape(ref)+"/subscribe", nil, &sub, requestOptions{})
	return sub, err
}

// GetSubscription is GET .../subscribe.
func (c *Client) GetSubscription(ctx context.Context, ref string) (Subscription, error) {
	prefix, err := subscriptionPathPrefix(ref)
	if err != nil {
		return Subscription{}, err
	}
	var sub Subscription
	err = c.do(ctx, http.MethodGet, prefix+url.PathEscape(ref)+"/subscribe", nil, &sub, requestOptions{})
	return sub, err
}
