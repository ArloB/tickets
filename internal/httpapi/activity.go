package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// activityEvent is GET /projects/{key}/activity's row shape (§5.10).
// entity is "" only for a project-level event (project_created/
// project_updated) — see service.ActivityEvent's doc.
type activityEvent struct {
	ID             int64     `json:"id"`
	Entity         string    `json:"entity,omitempty"`
	EntityKind     string    `json:"entity_kind"`
	Actor          string    `json:"actor"`
	EventType      string    `json:"event_type"`
	CommentID      *int64    `json:"comment_id,omitempty"`
	CommentExcerpt *string   `json:"comment_excerpt,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// activityCommentExcerptLimit bounds how much of a comment's body an
// activity row shows — enough to identify the comment at a glance
// without turning the feed into a second comment-reading surface.
const activityCommentExcerptLimit = 200

func toActivityEvent(e service.ActivityEvent) activityEvent {
	out := activityEvent{
		ID:         e.ID,
		Entity:     e.EntityRef,
		EntityKind: string(e.EntityKind),
		Actor:      e.Actor.String(),
		EventType:  e.EventType,
		CommentID:  e.CommentID,
		CreatedAt:  e.CreatedAt,
	}
	if e.CommentBody != nil {
		excerpt := *e.CommentBody
		if runes := []rune(excerpt); len(runes) > activityCommentExcerptLimit {
			// Truncate by rune, not byte: a byte-offset cut can land inside
			// a multi-byte UTF-8 character and corrupt the tail.
			excerpt = string(runes[:activityCommentExcerptLimit])
		}
		out.CommentExcerpt = &excerpt
	}
	return out
}

type activityPage struct {
	Events     []activityEvent `json:"events"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

func (s *Server) listActivity(w http.ResponseWriter, r *http.Request) {
	projectKey := r.PathValue("key")
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

	result, err := s.svc.ListActivity(r.Context(), projectKey, service.ActivityListFilters{
		Actor: q.Get("actor"), EntityKind: q.Get("entity_kind"), EventType: q.Get("event_type"),
	}, limit, q.Get("cursor"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]activityEvent, len(result.Events))
	for i, e := range result.Events {
		out[i] = toActivityEvent(e)
	}
	writeJSON(w, http.StatusOK, activityPage{Events: out, NextCursor: result.NextCursor})
}
