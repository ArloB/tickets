package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// TestPhase2ExitCriterion is the automated form of the Phase 2 plan's
// exit criterion: "two authenticated agents and one anonymous viewer
// receive correct... attribution." It spins up a full server
// composition (newAuthTestServer, the same one auth_test.go's suite
// uses), creates an admin account and two agents with distinct bearer
// tokens, enables anonymous read, and drives the representative
// workflow end to end — every response validated against
// api/openapi.yaml the same way every other test in this package is.
func TestPhase2ExitCriterion(t *testing.T) {
	ts, svc, _ := newAuthTestServer(t, true)
	mustCreateAdmin(t, svc, "alice", "correct-password")
	sessionID, csrfToken := ts.login("alice", "correct-password")
	adminHeaders := map[string]string{"Cookie": sessionCookieName + "=" + sessionID, "X-CSRF-Token": csrfToken}

	// --- two agents, two distinct bearer tokens ---
	tokenFor := func(agentName string) string {
		t.Helper()
		createResp, createBody := ts.doNoAuth(http.MethodPost, "/agents", adminHeaders, mustJSON(t, map[string]string{"name": agentName}))
		if createResp.StatusCode != http.StatusCreated {
			t.Fatalf("create agent %s: status = %d, body=%s", agentName, createResp.StatusCode, createBody)
		}
		tokenResp, tokenBody := ts.doNoAuth(http.MethodPost, "/agents/"+agentName+"/tokens", adminHeaders, mustJSON(t, map[string]string{"description": "exit criterion"}))
		if tokenResp.StatusCode != http.StatusCreated {
			t.Fatalf("create token for %s: status = %d, body=%s", agentName, tokenResp.StatusCode, tokenBody)
		}
		var created struct {
			ID    int64  `json:"id"`
			Token string `json:"token"`
		}
		if err := json.Unmarshal(tokenBody, &created); err != nil {
			t.Fatalf("unmarshal token response for %s: %v", agentName, err)
		}
		return created.Token
	}
	tokenA := tokenFor("codex")
	tokenB := tokenFor("claude")

	agentAHeaders := map[string]string{"Authorization": "Bearer " + tokenA}
	agentBHeaders := map[string]string{"Authorization": "Bearer " + tokenB}

	// --- setup: a project for the agents to work in ---
	projResp, projBody := ts.doNoAuth(http.MethodPost, "/projects", adminHeaders, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	if projResp.StatusCode != http.StatusCreated {
		t.Fatalf("create project: status = %d, body=%s", projResp.StatusCode, projBody)
	}

	// --- agent A creates a ticket: Creator must be agent:codex ---
	createResp, createBody := ts.doNoAuth(http.MethodPost, "/projects/ABC/tickets", agentAHeaders,
		mustJSON(t, map[string]any{"type": "task", "title": "Exit criterion ticket", "general": true}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("agent A create ticket: status = %d, body=%s", createResp.StatusCode, createBody)
	}
	var ticket map[string]any
	_ = json.Unmarshal(createBody, &ticket)
	ref, _ := ticket["ref"].(string)
	if ticket["creator"] != "agent:codex" {
		t.Errorf("ticket.creator = %v, want agent:codex", ticket["creator"])
	}

	// --- agent B comments on it: author must be agent:claude ---
	commentResp, commentBody := ts.doNoAuth(http.MethodPost, "/tickets/"+ref+"/comments", agentBHeaders,
		mustJSON(t, map[string]string{"body": "Reviewed."}))
	if commentResp.StatusCode != http.StatusCreated {
		t.Fatalf("agent B create comment: status = %d, body=%s", commentResp.StatusCode, commentBody)
	}
	var comment map[string]any
	_ = json.Unmarshal(commentBody, &comment)
	if comment["author"] != "agent:claude" {
		t.Errorf("comment.author = %v, want agent:claude", comment["author"])
	}

	// --- anonymous read succeeds ---
	anonGetResp, anonGetBody := ts.doNoAuth(http.MethodGet, "/tickets/"+ref, nil, nil)
	if anonGetResp.StatusCode != http.StatusOK {
		t.Fatalf("anonymous GET ticket: status = %d, body=%s", anonGetResp.StatusCode, anonGetBody)
	}

	// --- anonymous write is rejected ---
	anonPostResp, _ := ts.doNoAuth(http.MethodPost, "/tickets/"+ref+"/comments", nil, mustJSON(t, map[string]string{"body": "should be rejected"}))
	if anonPostResp.StatusCode != http.StatusForbidden {
		t.Errorf("anonymous POST comment: status = %d, want 403", anonPostResp.StatusCode)
	}

	// --- a revoked token is subsequently rejected ---
	listResp, listBody := ts.doNoAuth(http.MethodGet, "/agents/claude/tokens", adminHeaders, nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list claude's tokens: status = %d, body=%s", listResp.StatusCode, listBody)
	}
	var tokensPage struct {
		Tokens []struct{ ID int64 } `json:"tokens"`
	}
	_ = json.Unmarshal(listBody, &tokensPage)
	if len(tokensPage.Tokens) != 1 {
		t.Fatalf("claude's tokens = %s, want exactly one", listBody)
	}
	tokenID := tokensPage.Tokens[0].ID

	revokeResp, revokeBody := ts.doNoAuth(http.MethodDelete, "/agents/claude/tokens/"+strconv.FormatInt(tokenID, 10), adminHeaders, nil)
	if revokeResp.StatusCode != http.StatusOK {
		t.Fatalf("revoke agent B's token: status = %d, body=%s", revokeResp.StatusCode, revokeBody)
	}

	afterRevokeResp, _ := ts.doNoAuth(http.MethodGet, "/tickets/"+ref+"/comments", agentBHeaders, nil)
	if afterRevokeResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("agent B's revoked token: status = %d, want 401", afterRevokeResp.StatusCode)
	}
}
