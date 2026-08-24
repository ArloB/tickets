package main

import (
	"encoding/json"
	"testing"
)

func TestSubscribeAndUnsubscribeJSON(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ticketRef := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	out := captureStdout(t, func() {
		if err := runSubscribe([]string{ticketRef, "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runSubscribe: %v", err)
		}
	})
	var sub struct {
		Subscribed bool `json:"subscribed"`
	}
	if err := json.Unmarshal([]byte(out), &sub); err != nil {
		t.Fatalf("decode subscribe --json output: %v (raw: %s)", err, out)
	}
	if !sub.Subscribed {
		t.Fatalf("subscribe %q: subscribed = false, want true", ticketRef)
	}

	out = captureStdout(t, func() {
		if err := runUnsubscribe([]string{ticketRef, "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runUnsubscribe: %v", err)
		}
	})
	if err := json.Unmarshal([]byte(out), &sub); err != nil {
		t.Fatalf("decode unsubscribe --json output: %v (raw: %s)", err, out)
	}
	if sub.Subscribed {
		t.Fatalf("unsubscribe %q: subscribed = true, want false", ticketRef)
	}
}

func TestSubscribeRequiresAReference(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runSubscribe([]string{"--url", apiURL}); err == nil {
		t.Error("runSubscribe with no reference: want error, got nil")
	}
}

func TestNotificationsListAndReadJSON(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	// newTestAPIServerWithAgent's fixture ticket is created by the
	// human "local" actor, not this test's agent token — nothing has
	// assigned, mentioned, commented on, or changed anything the agent
	// is subscribed to, so its inbox starts empty. This also exercises
	// "read --all" against zero unread rows, which must succeed as a
	// no-op rather than error.
	out := captureStdout(t, func() {
		if err := runNotifications([]string{"list", "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runNotifications list: %v", err)
		}
	})
	var page struct {
		Notifications []map[string]any `json:"notifications"`
	}
	if err := json.Unmarshal([]byte(out), &page); err != nil {
		t.Fatalf("decode notifications list --json output: %v (raw: %s)", err, out)
	}
	if len(page.Notifications) != 0 {
		t.Fatalf("notifications list = %+v, want 0 (nothing notifies the agent about its own ticket)", page.Notifications)
	}

	out = captureStdout(t, func() {
		if err := runNotifications([]string{"read", "--all", "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runNotifications read --all: %v", err)
		}
	})
	var marked struct {
		Marked int64 `json:"marked"`
	}
	if err := json.Unmarshal([]byte(out), &marked); err != nil {
		t.Fatalf("decode notifications read --json output: %v (raw: %s)", err, out)
	}
	if marked.Marked != 0 {
		t.Fatalf("notifications read --all marked = %d, want 0", marked.Marked)
	}
}

func TestNotificationsReadRequiresIDsOrAll(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runNotifications([]string{"read", "--url", apiURL}); err == nil {
		t.Error("runNotifications read with no ids and no --all: want error, got nil")
	}
}

func TestNotificationsRequiresSubcommand(t *testing.T) {
	if err := runNotifications(nil); err == nil {
		t.Error("runNotifications with no subcommand: want error, got nil")
	}
}
