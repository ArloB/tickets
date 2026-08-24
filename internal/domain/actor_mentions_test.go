package domain

import (
	"reflect"
	"testing"
)

func TestScanActorMentions(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []ActorRef
	}{
		{
			name: "human mention",
			text: "cc @human:alice for review",
			want: []ActorRef{{ActorHuman, "alice"}},
		},
		{
			name: "agent and system mentions",
			text: "@agent:codex-1 and @system:system both saw this",
			want: []ActorRef{{ActorAgent, "codex-1"}, {ActorSystem, "system"}},
		},
		{
			name: "no bare-name shorthand",
			text: "@alice is not a mention without a kind",
			want: nil,
		},
		{
			name: "unrecognized kind is not a mention",
			text: "@robot:alice is not a valid kind",
			want: nil,
		},
		{
			name: "fenced code block excluded",
			text: "before\n```\n@human:alice\n```\nafter",
			want: nil,
		},
		{
			name: "inline code span excluded",
			text: "see `@human:alice` here",
			want: nil,
		},
		{
			name: "not a suffix of a longer word",
			text: "email@human:alice.example stays unmatched",
			want: nil,
		},
		{
			name: "duplicate mentions deduplicated, first-occurrence order",
			text: "@human:alice then @human:bob then @human:alice again",
			want: []ActorRef{{ActorHuman, "alice"}, {ActorHuman, "bob"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanActorMentions(tc.text)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ScanActorMentions(%q) = %+v, want %+v", tc.text, got, tc.want)
			}
		})
	}
}
