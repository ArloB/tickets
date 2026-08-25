package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// TestLinkAddListRemove exercises the whole tickets link group
// against a real server: add creates a link, list shows it, remove
// deletes it and a subsequent list is empty.
func TestLinkAddListRemove(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ticketRef := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	type linkJSON struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
		URL   string `json:"url"`
	}

	addOut := captureStdout(t, func() {
		if err := runLink([]string{
			"add", ticketRef, "--url", apiURL,
			"--title", "Design doc", "--link-url", "https://example.com/doc", "--json",
		}); err != nil {
			t.Fatalf("runLink add: %v", err)
		}
	})
	var added linkJSON
	if err := json.Unmarshal([]byte(addOut), &added); err != nil {
		t.Fatalf("unmarshal link add output: %v (raw: %s)", err, addOut)
	}
	if added.Title != "Design doc" || added.URL != "https://example.com/doc" {
		t.Errorf("link add result = %+v, want title=%q url=%q", added, "Design doc", "https://example.com/doc")
	}

	listOut := captureStdout(t, func() {
		if err := runLink([]string{"list", ticketRef, "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runLink list: %v", err)
		}
	})
	var links []linkJSON
	if err := json.Unmarshal([]byte(listOut), &links); err != nil {
		t.Fatalf("unmarshal link list output: %v (raw: %s)", err, listOut)
	}
	if len(links) != 1 || links[0].Title != "Design doc" {
		t.Fatalf("link list = %+v, want exactly one link titled %q", links, "Design doc")
	}

	removeOut := captureStdout(t, func() {
		if err := runLink([]string{"remove", ticketRef, strconv.FormatInt(added.ID, 10), "--url", apiURL}); err != nil {
			t.Fatalf("runLink remove: %v", err)
		}
	})
	if !strings.Contains(removeOut, "removed") {
		t.Errorf("link remove output = %q, want it to say removed", removeOut)
	}

	listAfterOut := captureStdout(t, func() {
		if err := runLink([]string{"list", ticketRef, "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runLink list (after remove): %v", err)
		}
	})
	var linksAfter []linkJSON
	if err := json.Unmarshal([]byte(listAfterOut), &linksAfter); err != nil {
		t.Fatalf("unmarshal link list output: %v (raw: %s)", err, listAfterOut)
	}
	if len(linksAfter) != 0 {
		t.Errorf("link list after remove = %+v, want empty", linksAfter)
	}
}

func TestLinkAddRequiresTitleAndURL(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ticketRef := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runLink([]string{"add", ticketRef, "--url", apiURL}); err == nil {
		t.Error("link add with no --title/--link-url: want error, got nil")
	}
	if err := runLink([]string{"add", ticketRef, "--url", apiURL, "--title", "x"}); err == nil {
		t.Error("link add with no --link-url: want error, got nil")
	}
}

func TestLinkRemoveRequiresIntegerID(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ticketRef := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runLink([]string{"remove", ticketRef, "not-a-number", "--url", apiURL}); err == nil {
		t.Error("link remove with a non-integer id: want error, got nil")
	}
}
