package domain

import (
	"fmt"
	"strings"
)

// ActorKind is who or what performed a mutation (product spec §4.1).
// Every mutation is attributed to exactly one actor — there is no
// "unattributed" state, even before ADR 0004's real authentication
// lands in Phase 2 (a seeded system/local actor fills that gap; see
// migration 0002_core_domain.sql).
type ActorKind string

const (
	ActorHuman  ActorKind = "human"
	ActorAgent  ActorKind = "agent"
	ActorSystem ActorKind = "system"
)

func (k ActorKind) Valid() bool {
	switch k {
	case ActorHuman, ActorAgent, ActorSystem:
		return true
	}
	return false
}

// ActorRef identifies an actor on the wire as "kind:name" (e.g.
// "agent:codex-1", "human:arlo", "system:system"), matching
// docs/contracts/representations.md's creator example. It is not the
// actor's internal surrogate id (never serialized, same rule as
// entities.id under ADR 0002) — callers resolve an ActorRef to an
// actor row in internal/store.
type ActorRef struct {
	Kind ActorKind
	Name string
}

// String renders the "kind:name" wire form. It does not validate —
// call Valid first if that matters to the caller.
func (a ActorRef) String() string {
	return string(a.Kind) + ":" + a.Name
}

// ParseActorRef is String's inverse. It rejects a missing separator,
// an empty name, or an unrecognized kind rather than guessing.
func ParseActorRef(s string) (ActorRef, error) {
	kind, name, found := strings.Cut(s, ":")
	if !found {
		return ActorRef{}, fmt.Errorf("domain: actor ref %q has no kind:name separator", s)
	}
	if name == "" {
		return ActorRef{}, fmt.Errorf("domain: actor ref %q has an empty name", s)
	}
	ref := ActorRef{Kind: ActorKind(kind), Name: name}
	if !ref.Kind.Valid() {
		return ActorRef{}, fmt.Errorf("domain: actor ref %q has unknown kind %q", s, kind)
	}
	return ref, nil
}
