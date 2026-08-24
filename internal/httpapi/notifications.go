package httpapi

import (
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// notificationView is one row of GET /notifications' response.
type notificationView struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	Entity      string     `json:"entity"`
	EntityKind  string     `json:"entity_kind"`
	CommentID   *int64     `json:"comment_id,omitempty"`
	TriggeredBy string     `json:"triggered_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	ReadAt      *time.Time `json:"read_at,omitempty"`
}

func toNotificationView(n service.Notification) notificationView {
	out := notificationView{
		ID: n.ID, Kind: n.Kind, Entity: n.Entity, EntityKind: string(n.EntityKind),
		CommentID: n.CommentID, CreatedAt: n.CreatedAt, ReadAt: n.ReadAt,
	}
	if n.TriggeredBy != nil {
		out.TriggeredBy = n.TriggeredBy.String()
	}
	return out
}

type notificationsPage struct {
	Notifications []notificationView `json:"notifications"`
	NextCursor    string             `json:"next_cursor,omitempty"`
}

// listNotifications is GET /notifications: the calling actor's own
// notifications, newest first (product spec §6.4/§6.5's inbox).
func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := 0
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Field: "limit", Message: "limit must be a non-negative integer"})
			return
		}
		limit = n
	}
	unreadOnly := q.Get("unread") == "true"

	result, err := s.svc.ListNotifications(r.Context(), requestActor(r), unreadOnly, limit, q.Get("cursor"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]notificationView, len(result.Notifications))
	for i, n := range result.Notifications {
		out[i] = toNotificationView(n)
	}
	writeJSON(w, http.StatusOK, notificationsPage{Notifications: out, NextCursor: result.NextCursor})
}

type markNotificationsReadRequest struct {
	IDs []int64 `json:"ids"`
	All bool    `json:"all"`
}

type markNotificationsReadResponse struct {
	Marked int64 `json:"marked"`
}

// markNotificationsRead is POST /notifications/read: marks either the
// named ids or (all: true) every currently-unread notification read.
func (s *Server) markNotificationsRead(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req markNotificationsReadRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	if !req.All && len(req.IDs) == 0 {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Field: "ids", Message: "either ids or all must be set"})
		return
	}

	n, err := s.svc.MarkNotificationsRead(r.Context(), service.MarkNotificationsReadRequest{IDs: req.IDs, All: req.All}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, markNotificationsReadResponse{Marked: n})
}
