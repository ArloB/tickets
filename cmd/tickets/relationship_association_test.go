package main

import (
	"context"
	"strings"
	"testing"

	"github.com/ArloB/tickets/internal/apiclient"
)

// createSecondTicket seeds a second ticket in project ABC directly
// through apiclient (bypassing the CLI, which has no `ticket create`
// client-mode subcommand yet — ticket creation is only exposed today
// via internal/mcpsrv's tools, not cmd/tickets), returning its ref for
// relate/associate tests that need two distinct entities.
func createSecondTicket(t *testing.T, apiURL, token string) string {
	t.Helper()
	c := &apiclient.Client{BaseURL: apiURL, Token: token}
	ticket, err := c.CreateTicket(context.Background(), "ABC", apiclient.CreateTicketRequest{
		Type: "task", Title: "Second ticket", Priority: "medium",
	})
	if err != nil {
		t.Fatalf("createSecondTicket: %v", err)
	}
	return ticket.Ref
}

func TestTicketRelateRelationshipsUnrelate(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref1 := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)
	ref2 := createSecondTicket(t, apiURL, token)

	captureStdout(t, func() {
		if err := runTicket([]string{"relate", ref1, "--url", apiURL, "--type", "blocked_by", "--target", ref2}); err != nil {
			t.Fatalf("runTicket relate: %v", err)
		}
	})

	out := captureStdout(t, func() {
		if err := runTicket([]string{"relationships", ref1, "--url", apiURL}); err != nil {
			t.Fatalf("runTicket relationships: %v", err)
		}
	})
	if !strings.Contains(out, "blocked_by") || !strings.Contains(out, ref2) {
		t.Errorf("ticket relationships output = %q, want it to list blocked_by %s", out, ref2)
	}

	captureStdout(t, func() {
		if err := runTicket([]string{"unrelate", ref1, "--url", apiURL, "--type", "blocked_by", "--target", ref2}); err != nil {
			t.Fatalf("runTicket unrelate: %v", err)
		}
	})

	afterOut := captureStdout(t, func() {
		if err := runTicket([]string{"relationships", ref1, "--url", apiURL}); err != nil {
			t.Fatalf("runTicket relationships after unrelate: %v", err)
		}
	})
	if strings.Contains(afterOut, "blocked_by") {
		t.Errorf("ticket relationships after unrelate = %q, want no blocked_by edge left", afterOut)
	}
}

func TestTicketAssociateAssociationsDisassociate(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref1 := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)
	ref2 := createSecondTicket(t, apiURL, token)

	captureStdout(t, func() {
		if err := runTicket([]string{"associate", ref1, "--url", apiURL, "--target", ref2}); err != nil {
			t.Fatalf("runTicket associate: %v", err)
		}
	})

	out := captureStdout(t, func() {
		if err := runTicket([]string{"associations", ref1, "--url", apiURL}); err != nil {
			t.Fatalf("runTicket associations: %v", err)
		}
	})
	if !strings.Contains(out, ref2) {
		t.Errorf("ticket associations output = %q, want it to list %s", out, ref2)
	}

	captureStdout(t, func() {
		if err := runTicket([]string{"disassociate", ref1, "--url", apiURL, "--target", ref2}); err != nil {
			t.Fatalf("runTicket disassociate: %v", err)
		}
	})

	afterOut := captureStdout(t, func() {
		if err := runTicket([]string{"associations", ref1, "--url", apiURL}); err != nil {
			t.Fatalf("runTicket associations after disassociate: %v", err)
		}
	})
	if strings.Contains(afterOut, ref2) {
		t.Errorf("ticket associations after disassociate = %q, want %s removed", afterOut, ref2)
	}
}

func TestTicketRelateRequiresTypeAndTarget(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref1 := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runTicket([]string{"relate", ref1, "--url", apiURL, "--target", "ABC-2"}); err == nil {
		t.Error("ticket relate with no --type: want error, got nil")
	}
	if err := runTicket([]string{"relate", ref1, "--url", apiURL, "--type", "blocks"}); err == nil {
		t.Error("ticket relate with no --target: want error, got nil")
	}
}

func TestTicketAssociateRequiresTarget(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref1 := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runTicket([]string{"associate", ref1, "--url", apiURL}); err == nil {
		t.Error("ticket associate with no --target: want error, got nil")
	}
}
