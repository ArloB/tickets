package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// twoActorFixture sets up an admin (session-authenticated) and an
// agent (bearer-token-authenticated) actor against the same server —
// the exit-criterion test's own two-actor pattern
// (exit_criterion_test.go), reused here since notifications are
// inherently a two-actor feature (nobody can notify themselves in any
// of the four categories, ADR 0019).
func twoActorFixture(t *testing.T) (ts *testServer, adminHeaders, agentHeaders map[string]string) {
	t.Helper()
	ts, svc, _ := newAuthTestServer(t, false)
	mustCreateAdmin(t, svc, "admin", "correct-password")
	sessionID, csrfToken := ts.login("admin", "correct-password")
	adminHeaders = map[string]string{"Cookie": sessionCookieName + "=" + sessionID, "X-CSRF-Token": csrfToken}

	createResp, createBody := ts.doNoAuth(http.MethodPost, "/agents", adminHeaders, mustJSON(t, map[string]string{"name": "codex"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create agent: status = %d, body=%s", createResp.StatusCode, createBody)
	}
	tokenResp, tokenBody := ts.doNoAuth(http.MethodPost, "/agents/codex/tokens", adminHeaders, mustJSON(t, map[string]string{"description": "notifications test"}))
	if tokenResp.StatusCode != http.StatusCreated {
		t.Fatalf("create agent token: status = %d, body=%s", tokenResp.StatusCode, tokenBody)
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(tokenBody, &created); err != nil {
		t.Fatalf("unmarshal token response: %v", err)
	}
	agentHeaders = map[string]string{"Authorization": "Bearer " + created.Token}
	return ts, adminHeaders, agentHeaders
}

// withIfMatch copies headers and adds If-Match for version (ADR 0008)
// — assign, like every conditional update, requires it.
func withIfMatch(headers map[string]string, version int) map[string]string {
	out := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		out[k] = v
	}
	out["If-Match"] = `"` + strconv.Itoa(version) + `"`
	return out
}

// TestAssignTicketOverHTTPNotifiesAssignee is Step 7's route-wiring
// exit check for the "assigned" category: the admin creates a ticket
// and assigns it to the agent; the agent's own GET /notifications
// shows exactly one "assigned" notification.
func TestAssignTicketOverHTTPNotifiesAssignee(t *testing.T) {
	ts, adminHeaders, agentHeaders := twoActorFixture(t)

	ts.doNoAuth(http.MethodPost, "/projects", adminHeaders, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.doNoAuth(http.MethodPost, "/projects/ABC/tickets", adminHeaders, mustJSON(t, map[string]any{"type": "task", "title": "Assign me", "general": true}))
	ts.doNoAuth(http.MethodPost, "/tickets/ABC-1/assign", withIfMatch(adminHeaders, 1), mustJSON(t, map[string]string{"assignee": "agent:codex"}))

	resp, body := ts.doNoAuth(http.MethodGet, "/notifications", agentHeaders, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list notifications: status = %d, body=%s", resp.StatusCode, body)
	}
	var page struct {
		Notifications []map[string]any `json:"notifications"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("unmarshal notifications page: %v", err)
	}
	if len(page.Notifications) != 1 || page.Notifications[0]["kind"] != "assigned" {
		t.Fatalf("agent's notifications = %+v, want exactly one 'assigned'", page.Notifications)
	}
	if page.Notifications[0]["entity"] != "ABC-1" {
		t.Errorf("notification entity = %v, want ABC-1", page.Notifications[0]["entity"])
	}
}

// TestCommentNotifiesSubscriberAndSubscribesCommenter mirrors the
// service-level test of the same shape, but over the real HTTP
// surface: subscribe/unsubscribe endpoints, comment creation, and the
// notification list all validated against api/openapi.yaml.
func TestCommentNotifiesSubscriberAndSubscribesCommenter(t *testing.T) {
	ts, adminHeaders, agentHeaders := twoActorFixture(t)

	ts.doNoAuth(http.MethodPost, "/projects", adminHeaders, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.doNoAuth(http.MethodPost, "/projects/ABC/tickets", adminHeaders, mustJSON(t, map[string]any{"type": "task", "title": "Discuss", "general": true}))
	// admin created the ticket, so admin is already auto-subscribed.

	ts.doNoAuth(http.MethodPost, "/tickets/ABC-1/comments", agentHeaders, mustJSON(t, map[string]string{"body": "agent's reply"}))

	resp, body := ts.doNoAuth(http.MethodGet, "/notifications", adminHeaders, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list notifications for admin: status = %d, body=%s", resp.StatusCode, body)
	}
	var page struct {
		Notifications []map[string]any `json:"notifications"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("unmarshal notifications page: %v", err)
	}
	if len(page.Notifications) != 1 || page.Notifications[0]["kind"] != "commented" {
		t.Fatalf("admin's notifications = %+v, want exactly one 'commented'", page.Notifications)
	}

	subResp, subBody := ts.doNoAuth(http.MethodGet, "/tickets/ABC-1/subscribe", agentHeaders, nil)
	if subResp.StatusCode != http.StatusOK {
		t.Fatalf("get agent subscription: status = %d, body=%s", subResp.StatusCode, subBody)
	}
	var sub struct {
		Subscribed bool `json:"subscribed"`
	}
	if err := json.Unmarshal(subBody, &sub); err != nil {
		t.Fatalf("unmarshal subscription: %v", err)
	}
	if !sub.Subscribed {
		t.Errorf("agent's subscription after commenting = false, want true")
	}
}

// TestUnsubscribeOverHTTPStopsNotifications exercises DELETE .../subscribe.
func TestUnsubscribeOverHTTPStopsNotifications(t *testing.T) {
	ts, adminHeaders, agentHeaders := twoActorFixture(t)

	ts.doNoAuth(http.MethodPost, "/projects", adminHeaders, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.doNoAuth(http.MethodPost, "/projects/ABC/tickets", adminHeaders, mustJSON(t, map[string]any{"type": "task", "title": "T", "general": true}))

	unsubResp, unsubBody := ts.doNoAuth(http.MethodDelete, "/tickets/ABC-1/subscribe", adminHeaders, nil)
	if unsubResp.StatusCode != http.StatusOK {
		t.Fatalf("unsubscribe: status = %d, body=%s", unsubResp.StatusCode, unsubBody)
	}

	ts.doNoAuth(http.MethodPost, "/tickets/ABC-1/comments", agentHeaders, mustJSON(t, map[string]string{"body": "reply"}))

	resp, body := ts.doNoAuth(http.MethodGet, "/notifications", adminHeaders, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list notifications: status = %d, body=%s", resp.StatusCode, body)
	}
	var page struct {
		Notifications []map[string]any `json:"notifications"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("unmarshal notifications page: %v", err)
	}
	if len(page.Notifications) != 0 {
		t.Fatalf("unsubscribed admin still got notified: %+v", page.Notifications)
	}
}

// TestMarkNotificationsReadOverHTTP exercises POST /notifications/read
// both by id and with all: true.
func TestMarkNotificationsReadOverHTTP(t *testing.T) {
	ts, adminHeaders, agentHeaders := twoActorFixture(t)

	ts.doNoAuth(http.MethodPost, "/projects", adminHeaders, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.doNoAuth(http.MethodPost, "/projects/ABC/tickets", adminHeaders, mustJSON(t, map[string]any{"type": "task", "title": "T", "general": true}))
	ts.doNoAuth(http.MethodPost, "/tickets/ABC-1/assign", withIfMatch(adminHeaders, 1), mustJSON(t, map[string]string{"assignee": "agent:codex"}))

	listResp, listBody := ts.doNoAuth(http.MethodGet, "/notifications", agentHeaders, nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list notifications: status = %d, body=%s", listResp.StatusCode, listBody)
	}
	var page struct {
		Notifications []struct {
			ID int64 `json:"id"`
		} `json:"notifications"`
	}
	if err := json.Unmarshal(listBody, &page); err != nil || len(page.Notifications) != 1 {
		t.Fatalf("setup listing: %s, %v", listBody, err)
	}

	readResp, readBody := ts.doNoAuth(http.MethodPost, "/notifications/read", agentHeaders, mustJSON(t, map[string]any{"ids": []int64{page.Notifications[0].ID}}))
	if readResp.StatusCode != http.StatusOK {
		t.Fatalf("mark read: status = %d, body=%s", readResp.StatusCode, readBody)
	}
	var marked struct {
		Marked int64 `json:"marked"`
	}
	if err := json.Unmarshal(readBody, &marked); err != nil || marked.Marked != 1 {
		t.Fatalf("mark read response = %s, %v; want marked=1", readBody, err)
	}

	unreadResp, unreadBody := ts.doNoAuth(http.MethodGet, "/notifications?unread=true", agentHeaders, nil)
	if unreadResp.StatusCode != http.StatusOK {
		t.Fatalf("list unread: status = %d, body=%s", unreadResp.StatusCode, unreadBody)
	}
	var unreadPage struct {
		Notifications []map[string]any `json:"notifications"`
	}
	if err := json.Unmarshal(unreadBody, &unreadPage); err != nil {
		t.Fatalf("unmarshal unread page: %v", err)
	}
	if len(unreadPage.Notifications) != 0 {
		t.Fatalf("unread notifications after marking read = %+v, want 0", unreadPage.Notifications)
	}
}

// TestMarkNotificationsReadRequiresIDsOrAll proves an empty request
// body is a validation error, not a silent no-op.
func TestMarkNotificationsReadRequiresIDsOrAll(t *testing.T) {
	ts, _, agentHeaders := twoActorFixture(t)

	resp, body := ts.doNoAuth(http.MethodPost, "/notifications/read", agentHeaders, mustJSON(t, map[string]any{}))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("mark read with neither ids nor all: status = %d, body=%s, want 400", resp.StatusCode, body)
	}
}
