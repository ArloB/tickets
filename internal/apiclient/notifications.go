package apiclient

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Notification mirrors internal/httpapi/notifications.go's
// notificationView.
type Notification struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	Entity      string     `json:"entity"`
	EntityKind  string     `json:"entity_kind"`
	CommentID   *int64     `json:"comment_id,omitempty"`
	TriggeredBy string     `json:"triggered_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	ReadAt      *time.Time `json:"read_at,omitempty"`
}

// NotificationsPage is GET /notifications' response envelope.
type NotificationsPage struct {
	Notifications []Notification `json:"notifications"`
	NextCursor    string         `json:"next_cursor,omitempty"`
}

// ListNotifications is GET /notifications.
func (c *Client) ListNotifications(ctx context.Context, unreadOnly bool, limit int, cursor string) (NotificationsPage, error) {
	extra := url.Values{}
	if unreadOnly {
		extra.Set("unread", "true")
	}
	var page NotificationsPage
	err := c.do(ctx, http.MethodGet, "/notifications"+listQuery(extra, limit, cursor), nil, &page, requestOptions{})
	return page, err
}

// MarkNotificationsRead is POST /notifications/read: exactly the
// named ids, or (all: true) every currently-unread notification.
func (c *Client) MarkNotificationsRead(ctx context.Context, ids []int64, all bool) (int64, error) {
	var resp struct {
		Marked int64 `json:"marked"`
	}
	err := c.do(ctx, http.MethodPost, "/notifications/read",
		struct {
			IDs []int64 `json:"ids,omitempty"`
			All bool    `json:"all,omitempty"`
		}{IDs: ids, All: all},
		&resp, requestOptions{})
	return resp.Marked, err
}
