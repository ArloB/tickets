package httpapi

import (
	"bufio"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ArloB/tickets/internal/service"
)

func TestHubPublishDeliversToSubscriber(t *testing.T) {
	h := NewHub()
	id, ch, ok := h.subscribe("")
	if !ok {
		t.Fatal("subscribe on an open hub should succeed")
	}
	defer h.unsubscribe(id)

	h.Publish(service.ChangeHint{Kind: service.HintEntityChanged, Ref: "ABC-1", Project: "ABC"})

	select {
	case hint := <-ch:
		if hint.Ref != "ABC-1" || hint.Project != "ABC" {
			t.Errorf("hint = %#v, want ref ABC-1/project ABC", hint)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for published hint")
	}
}

func TestHubNotificationsChangedScopedToActor(t *testing.T) {
	h := NewHub()
	aliceID, aliceCh, _ := h.subscribe("human:alice")
	defer h.unsubscribe(aliceID)
	bobID, bobCh, _ := h.subscribe("human:bob")
	defer h.unsubscribe(bobID)

	h.Publish(service.ChangeHint{Kind: service.HintNotificationsChanged, Actor: "human:alice"})

	select {
	case hint := <-aliceCh:
		if hint.Actor != "human:alice" {
			t.Errorf("alice's hint actor = %q, want human:alice", hint.Actor)
		}
	case <-time.After(time.Second):
		t.Fatal("alice never received her own notifications_changed hint")
	}

	select {
	case hint := <-bobCh:
		t.Fatalf("bob should not receive alice's notification hint, got %#v", hint)
	case <-time.After(100 * time.Millisecond):
		// expected: nothing delivered to bob
	}
}

func TestHubEntityChangedGoesToEveryoneIncludingAnonymous(t *testing.T) {
	h := NewHub()
	anonID, anonCh, _ := h.subscribe("")
	defer h.unsubscribe(anonID)

	h.Publish(service.ChangeHint{Kind: service.HintEntityChanged, Ref: "ABC-1", Project: "ABC"})

	select {
	case <-anonCh:
	case <-time.After(time.Second):
		t.Fatal("an anonymous subscriber should still receive entity_changed hints")
	}
}

func TestHubCloseUnblocksSubscribers(t *testing.T) {
	h := NewHub()
	_, ch, _ := h.subscribe("")

	h.Close()

	select {
	case _, open := <-ch:
		if open {
			t.Fatal("channel should be closed, not deliver a value")
		}
	case <-time.After(time.Second):
		t.Fatal("Close should close every subscriber channel immediately")
	}

	if _, _, ok := h.subscribe(""); ok {
		t.Error("subscribe on a closed hub should fail")
	}
}

func TestHubPublishDropsRatherThanBlocksOnFullSubscriber(t *testing.T) {
	h := NewHub()
	id, ch, _ := h.subscribe("")
	defer h.unsubscribe(id)

	done := make(chan struct{})
	go func() {
		for i := 0; i < hubSubscriberBuffer+10; i++ {
			h.Publish(service.ChangeHint{Kind: service.HintEntityChanged, Ref: "ABC-1", Project: "ABC"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked instead of dropping hints for a full subscriber channel")
	}
	_ = ch
}

// TestEventsEndpointStreamsEntityChangedHint exercises the full route:
// a real SSE connection over the test server, unblocked by a mutation
// made through the ordinary REST API while the stream is open. Uses a
// raw client rather than testServer.do/doUnvalidated (both read the
// full response body before returning, which would hang forever
// against an indefinite text/event-stream body).
func TestEventsEndpointStreamsEntityChangedHint(t *testing.T) {
	ts := newTestServer(t)

	req, err := http.NewRequest(http.MethodGet, ts.url+"/api/v1/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ts.sessionID})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	reader := bufio.NewReader(resp.Body)
	lines := make(chan string, 16)
	go func() {
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				lines <- line
			}
			if err != nil {
				close(lines)
				return
			}
		}
	}()

	body := mustJSON(t, map[string]string{"key": "SSE", "title": "SSE Test"})
	projResp, projBody := ts.do(http.MethodPost, "/projects", nil, body)
	if projResp.StatusCode != http.StatusCreated {
		t.Fatalf("create project: status %d, body %s", projResp.StatusCode, projBody)
	}

	ticketBody := mustJSON(t, map[string]any{"type": "task", "title": "Watch me", "general": true})
	tResp, tBody := ts.do(http.MethodPost, "/projects/SSE/tickets", nil, ticketBody)
	if tResp.StatusCode != http.StatusCreated {
		t.Fatalf("create ticket: status %d, body %s", tResp.StatusCode, tBody)
	}

	deadline := time.After(5 * time.Second)
	var sawEntityChanged, sawTicketRef bool
	var buf strings.Builder
	for !sawTicketRef {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("stream closed before seeing an entity_changed hint; collected:\n%s", buf.String())
			}
			buf.WriteString(line)
			if strings.HasPrefix(line, "event: "+service.HintEntityChanged) {
				sawEntityChanged = true
			}
			if sawEntityChanged && strings.Contains(line, `"ref"`) {
				sawTicketRef = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for an entity_changed hint; collected:\n%s", buf.String())
		}
	}
}
