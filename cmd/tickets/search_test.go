package main

import (
	"encoding/json"
	"testing"
)

func TestSearchJSON(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ticketRef := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	out := captureStdout(t, func() {
		if err := runSearch([]string{"parser", "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runSearch: %v", err)
		}
	})
	var page struct {
		Hits []map[string]any `json:"hits"`
	}
	if err := json.Unmarshal([]byte(out), &page); err != nil {
		t.Fatalf("decode search --json output: %v (raw: %s)", err, out)
	}
	if len(page.Hits) != 1 || page.Hits[0]["ref"] != ticketRef {
		t.Fatalf("search %q hits = %+v, want exactly one hit for %s", "parser", page.Hits, ticketRef)
	}
}

func TestSearchRequiresAQuery(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runSearch([]string{"--url", apiURL}); err == nil {
		t.Error("runSearch with no query: want error, got nil")
	}
}

func TestSearchKindFilter(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	out := captureStdout(t, func() {
		if err := runSearch([]string{"parser", "--url", apiURL, "--kind", "feature", "--json"}); err != nil {
			t.Fatalf("runSearch: %v", err)
		}
	})
	var page struct {
		Hits []map[string]any `json:"hits"`
	}
	if err := json.Unmarshal([]byte(out), &page); err != nil {
		t.Fatalf("decode search --json output: %v (raw: %s)", err, out)
	}
	if len(page.Hits) != 0 {
		t.Fatalf("search %q --kind feature hits = %+v, want 0 (the match is a ticket, not a feature)", "parser", page.Hits)
	}
}
