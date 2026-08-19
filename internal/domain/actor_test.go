package domain

import "testing"

func TestActorRefStringRoundTrip(t *testing.T) {
	cases := []ActorRef{
		{Kind: ActorAgent, Name: "codex-1"},
		{Kind: ActorHuman, Name: "arlo"},
		{Kind: ActorSystem, Name: "system"},
	}
	for _, ref := range cases {
		s := ref.String()
		got, err := ParseActorRef(s)
		if err != nil {
			t.Errorf("ParseActorRef(%q): %v", s, err)
			continue
		}
		if got != ref {
			t.Errorf("ParseActorRef(String(%+v)) = %+v, want %+v", ref, got, ref)
		}
	}
}

func TestActorRefString(t *testing.T) {
	if got := (ActorRef{Kind: ActorAgent, Name: "codex-1"}).String(); got != "agent:codex-1" {
		t.Errorf(`String() = %q, want "agent:codex-1"`, got)
	}
}

func TestParseActorRefErrors(t *testing.T) {
	cases := []string{
		"no-separator",
		"agent:",
		"bogus:name",
		"",
		":name",
	}
	for _, s := range cases {
		if _, err := ParseActorRef(s); err == nil {
			t.Errorf("ParseActorRef(%q) = nil error, want error", s)
		}
	}
}

func TestActorKindValid(t *testing.T) {
	for _, k := range []ActorKind{ActorHuman, ActorAgent, ActorSystem} {
		if !k.Valid() {
			t.Errorf("%s.Valid() = false, want true", k)
		}
	}
	if ActorKind("bogus").Valid() {
		t.Error(`ActorKind("bogus").Valid() = true, want false`)
	}
}
