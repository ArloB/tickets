package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ArloB/tickets/internal/auth"
	"github.com/ArloB/tickets/internal/service"
)

// hubSubscriberBuffer is each SSE connection's outgoing hint queue.
// service.Broadcaster.Publish is called from inside every mutating
// service method's outer call site (never from within a transaction —
// see events.go's doc in internal/service), so it must never block on
// a slow browser: a full channel drops the hint rather than stalling
// the mutation that produced it. A dropped hint is not a correctness
// problem — product spec §17's risk table already requires the client
// to refetch authoritative state rather than trust the stream, so a
// missed hint is recovered by the connection's own periodic keep-alive
// churn or the next hint for the same entity, never by SSE itself.
const hubSubscriberBuffer = 16

// keepAliveInterval is how often events sends an SSE comment line to
// keep an idle connection (and any intermediate proxy) from timing
// out. Well under typical 60s proxy/load-balancer idle timeouts.
const keepAliveInterval = 25 * time.Second

// hubSubscriber is one live SSE connection's mailbox. actor is the
// "kind:name" ActorRef string the connection authenticated as, or ""
// for an anonymous viewer — Hub.Publish uses it to gate
// HintNotificationsChanged delivery (events.go's package doc).
type hubSubscriber struct {
	ch    chan service.ChangeHint
	actor string
}

// Hub is the process-local, in-memory SSE fan-out that backs
// GET /api/v1/events (ADR 0020, Phase 5 Step 8). It implements
// service.Broadcaster, and is wired into the running Service via
// Service.SetBroadcaster in cmd/tickets/server.go — a CLI-only
// process or a test never registers one, so every service mutation's
// s.broadcast/publishNotified call is a safe no-op there.
//
// A single in-process hub is sufficient for the product's single-
// server deployment model (product spec §8.1/§11); a multi-instance
// deployment would need a shared pub/sub backend instead, out of
// scope here.
type Hub struct {
	mu     sync.Mutex
	subs   map[int64]*hubSubscriber
	nextID int64
	closed bool
}

// NewHub constructs an empty, open Hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[int64]*hubSubscriber)}
}

// Publish fans hint out to every live subscriber, filtering
// HintNotificationsChanged to the one connection (if any) whose actor
// matches hint.Actor — every other hint kind (HintEntityChanged) goes
// to every subscriber, including anonymous viewers, since an entity
// change is not actor-scoped information.
func (h *Hub) Publish(hint service.ChangeHint) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sub := range h.subs {
		if hint.Kind == service.HintNotificationsChanged && hint.Actor != sub.actor {
			continue
		}
		select {
		case sub.ch <- hint:
		default:
			// Slow subscriber: drop rather than block Publish's caller
			// (see hubSubscriberBuffer's doc).
		}
	}
}

// subscribe registers a new connection and returns its id and mailbox.
// ok is false once the hub has been closed (server shutting down) —
// the caller should respond 503 rather than open a channel that will
// never see events.
func (h *Hub) subscribe(actor string) (id int64, ch chan service.ChangeHint, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return 0, nil, false
	}
	id = h.nextID
	h.nextID++
	ch = make(chan service.ChangeHint, hubSubscriberBuffer)
	h.subs[id] = &hubSubscriber{ch: ch, actor: actor}
	return id, ch, true
}

// unsubscribe removes and closes one connection's mailbox — called
// from the events handler's deferred cleanup on every exit path
// (client disconnect, hub already closed, handler error).
func (h *Hub) unsubscribe(id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if sub, ok := h.subs[id]; ok {
		delete(h.subs, id)
		close(sub.ch)
	}
}

// Close shuts the hub down: every live subscriber's mailbox is
// closed, which unblocks its events handler goroutine (waiting on a
// channel receive) so the connection ends promptly. net/http's own
// srv.Shutdown does not forcibly end a long-running handler on its
// own — it only stops accepting new connections and waits for
// in-flight ones to finish naturally — so cmd/tickets/server.go calls
// this itself as part of graceful shutdown (product spec §11), rather
// than leaving every open SSE connection to block until the shutdown
// timeout is reached (or forever, with no timeout at all).
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for id, sub := range h.subs {
		close(sub.ch)
		delete(h.subs, id)
	}
}

// changeHintWire is the JSON shape sent over the wire for one SSE
// "data:" line — a direct, minimal mirror of service.ChangeHint
// (empty fields omitted rather than sent as "").  Kind is also sent
// as the SSE "event:" field so a client can use EventSource's native
// addEventListener(kind, ...) instead of parsing every message's body
// to dispatch it.
type changeHintWire struct {
	Ref     string `json:"ref,omitempty"`
	Project string `json:"project,omitempty"`
	Actor   string `json:"actor,omitempty"`
}

// events is GET /api/v1/events: a Server-Sent Events stream of
// service.ChangeHint invalidation signals (product spec §6.4, ADR
// 0020). routeViewer, not routeEditor — an anonymous viewer may
// subscribe like any other GET, but subscribe() only ever passes a
// non-empty actor (gating HintNotificationsChanged delivery) when
// requestActor's permission is Editor, mirroring auth.go's me
// handler's own anonymous-detection convention.
//
// The stream never carries anything but a hint: product spec §17's
// risk table — "treat SSE as invalidation/change hints only; refetch
// authoritative state from the API" — is enforced here structurally,
// not just by convention, since changeHintWire has no field for a
// changed value to ever leak into.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	actor := ""
	if p := auth.FromContext(r.Context()); p.Permission == auth.PermissionEditor {
		actor = p.Actor.String()
	}

	id, ch, ok := s.hub.subscribe(actor)
	if !ok {
		http.Error(w, "server shutting down", http.StatusServiceUnavailable)
		return
	}
	defer s.hub.unsubscribe(id)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	keepAlive := time.NewTicker(keepAliveInterval)
	defer keepAlive.Stop()

	for {
		select {
		case hint, open := <-ch:
			if !open {
				return // Hub.Close: server shutting down
			}
			data, err := json.Marshal(changeHintWire{Ref: hint.Ref, Project: hint.Project, Actor: hint.Actor})
			if err != nil {
				logger.Error("httpapi: encode change hint", "error", err)
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", hint.Kind, data); err != nil {
				return // client gone
			}
			flusher.Flush()
		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return // client gone
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
