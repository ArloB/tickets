package service

import (
	"context"

	"github.com/ArloB/tickets/internal/store"
)

// ChangeHint is the payload published to live SSE subscribers (Phase
// 5 Step 8, ADR 0020). Product spec §17's risk table: "treat SSE as
// invalidation/change hints only; refetch authoritative state from
// the API" — a hint carries just enough to know *what* to refetch,
// never the changed value itself, so a client can never mistake the
// stream for authoritative state.
type ChangeHint struct {
	// Kind is HintEntityChanged or HintNotificationsChanged.
	Kind string
	// Ref and Project are set for HintEntityChanged: the entity's
	// formatted reference and owning project key, letting a client
	// scoped to one board/detail page filter to hints it cares about.
	Ref     string
	Project string
	// Actor is set for HintNotificationsChanged: the recipient's
	// formatted ActorRef ("kind:name") whose notification inbox
	// changed — never broadcast to every connection, only delivered to
	// that actor's own subscription (internal/httpapi/events.go).
	Actor string
}

const (
	HintEntityChanged        = "entity_changed"
	HintNotificationsChanged = "notifications_changed"
)

// Broadcaster publishes ChangeHints to whatever live-delivery
// mechanism the caller wires up (internal/httpapi's SSE hub in
// practice; nil in every test and CLI-only path that never registers
// one — Service.broadcast and Service.publishNotified are both
// nil-safe).
type Broadcaster interface {
	Publish(hint ChangeHint)
}

// SetBroadcaster registers b as the destination for every change hint
// this Service's mutations publish from here on. A setter rather than
// a New(...) constructor parameter — like blobs, most callers
// (tests, CLI commands with no live server) never need one, and this
// avoids touching every existing New(store, blobs) call site for a
// dependency that's optional everywhere except cmd/tickets/server.go.
func (s *Service) SetBroadcaster(b Broadcaster) {
	s.events = b
}

// CloseBroadcaster releases the registered Broadcaster's live
// connections, if it exposes a Close() method (internal/httpapi's Hub
// does). Called from cmd/tickets/server.go as part of graceful
// shutdown (product spec §11) — net/http's own srv.Shutdown does not
// forcibly end a long-running SSE handler on its own, so a Broadcaster
// gets an explicit chance to unblock every subscriber itself. A no-op
// when no broadcaster is registered, or it doesn't support closing.
func (s *Service) CloseBroadcaster() {
	if c, ok := s.events.(interface{ Close() }); ok {
		c.Close()
	}
}

// broadcast publishes hint if a Broadcaster is registered. Every
// mutation method calls this only after its own s.withTx has already
// returned a nil error — never from inside the transaction closure
// itself, so a hint is never published for a mutation that rolled
// back.
func (s *Service) broadcast(hint ChangeHint) {
	if s.events == nil || hint.Kind == "" {
		return
	}
	s.events.Publish(hint)
}

// publishNotified resolves each committed notification recipient's
// actor id to its public ActorRef and publishes one
// HintNotificationsChanged per recipient. Called the same way as
// broadcast: only after the transaction that inserted those
// notifications has already committed.
func (s *Service) publishNotified(ctx context.Context, recipientActorIDs []int64) {
	if s.events == nil {
		return
	}
	for _, id := range recipientActorIDs {
		ref, err := store.GetActorRefByID(ctx, s.store.DB(), id)
		if err != nil {
			// Best-effort: a live hint is never authoritative (the
			// recipient's own next poll/refetch still finds the
			// notification), so a resolution failure here isn't worth
			// surfacing as a mutation error this late.
			continue
		}
		s.events.Publish(ChangeHint{Kind: HintNotificationsChanged, Actor: ref.String()})
	}
}
