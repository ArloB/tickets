package main

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// TestTicketGetJSONKeySetIsStable is a golden-shape test: it pins the
// exact set of top-level keys `ticket get --json` (no --fields) emits,
// so an accidental field addition/removal/rename to apiclient.Ticket
// fails a test here instead of only being noticed by a human diffing
// output by eye. Deliberately checks the key set, not exact values —
// created_at/updated_at aren't deterministic across runs.
func TestTicketGetJSONKeySetIsStable(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	out := captureStdout(t, func() {
		if err := runTicket([]string{"get", ref, "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runTicket get: %v", err)
		}
	})
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("decode ticket get --json output: %v (raw: %s)", err, out)
	}
	got := make([]string, 0, len(decoded))
	for k := range decoded {
		got = append(got, k)
	}
	sort.Strings(got)

	want := []string{
		"created_at", "creator", "description", "feature", "priority",
		"project", "ref", "status", "title", "type", "updated_at", "version",
	}
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("ticket get --json keys = %v, want exactly %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ticket get --json keys = %v, want exactly %v", got, want)
			break
		}
	}
}

func TestTicketGetJSON(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	out := captureStdout(t, func() {
		if err := runTicket([]string{"get", ref, "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runTicket get: %v", err)
		}
	})
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("decode ticket get --json output: %v (raw: %s)", err, out)
	}
	if decoded["ref"] != ref || decoded["status"] == nil {
		t.Errorf("ticket get output = %v, want ref=%q and a status", decoded, ref)
	}
}

// TestTicketGetFieldsProjectsExactKeySet is the discriminating check
// for --fields' wiring: a typed-decode regression (accidentally
// routing through GetTicket instead of GetTicketFields) would produce
// extra zero-valued keys, which a presence-only assertion wouldn't
// catch — so this asserts the key set is exactly what was requested,
// nothing more.
func TestTicketGetFieldsProjectsExactKeySet(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	out := captureStdout(t, func() {
		if err := runTicket([]string{"get", ref, "--url", apiURL, "--fields", "ref,title", "--json"}); err != nil {
			t.Fatalf("runTicket get --fields: %v", err)
		}
	})
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("decode ticket get --fields --json output: %v (raw: %s)", err, out)
	}
	if len(decoded) != 2 {
		t.Fatalf("ticket get --fields ref,title output has %d keys, want exactly 2 (%v)", len(decoded), decoded)
	}
	if decoded["ref"] != ref {
		t.Errorf("ticket get --fields output ref = %v, want %q", decoded["ref"], ref)
	}
	if _, ok := decoded["title"]; !ok {
		t.Errorf("ticket get --fields ref,title output = %v, want a title key", decoded)
	}
}

func TestTicketGetIncludeComments(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	captureStdout(t, func() {
		if err := runComment([]string{"add", ref, "--url", apiURL, "--body", "hello"}); err != nil {
			t.Fatalf("runComment add: %v", err)
		}
	})

	out := captureStdout(t, func() {
		if err := runTicket([]string{"get", ref, "--url", apiURL, "--include", "comments", "--json"}); err != nil {
			t.Fatalf("runTicket get --include comments: %v", err)
		}
	})
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("decode ticket get --include --json output: %v (raw: %s)", err, out)
	}
	comments, ok := decoded["comments"].([]any)
	if !ok || len(comments) != 1 {
		t.Errorf("ticket get --include comments output comments = %v, want a one-element list", decoded["comments"])
	}
}

// TestTicketGetRejectsUnknownInclude closes a false-success gap: the
// server-side includeNames handler only checks include["comments"]/
// include["relationships"] and silently ignores anything else, so a
// typo like --include coments would otherwise exit 0 with no comments
// key — indistinguishable from a ticket that genuinely has none.
// Unlike --fields, there's no server-side validation_failed to defer
// to here (there are exactly two legal values), so the CLI checks it.
func TestTicketGetRejectsUnknownInclude(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runTicket([]string{"get", ref, "--url", apiURL, "--include", "coments"}); err == nil {
		t.Error("ticket get --include coments (typo): want error, got nil")
	}
}

func TestTicketGetRejectsUnknownField(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runTicket([]string{"get", ref, "--url", apiURL, "--fields", "not-a-real-field"}); err == nil {
		t.Error("ticket get --fields not-a-real-field: want an error from the server, got nil")
	}
}

func TestTicketGetRequiresLeadingRef(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runTicket([]string{"get", "--url", apiURL}); err == nil {
		t.Error("ticket get with no leading ref: want error, got nil")
	}
}

// TestTicketGetFieldsTableHonorsRequestedOrderJSONDoesNot pins the
// asymmetry docs/contracts/cli.md documents: the non-JSON table
// renders --fields columns in the order given, but --json's output
// goes through map[string]any, and encoding/json always sorts map
// keys alphabetically regardless of insertion order — so requesting
// "title,ref" still yields {"ref":...,"title":...} in JSON.
func TestTicketGetFieldsTableHonorsRequestedOrderJSONDoesNot(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	tableOut := captureStdout(t, func() {
		if err := runTicket([]string{"get", ref, "--url", apiURL, "--fields", "title,ref"}); err != nil {
			t.Fatalf("runTicket get --fields title,ref: %v", err)
		}
	})
	titleCol := strings.Index(tableOut, "TITLE")
	refCol := strings.Index(tableOut, "REF")
	if titleCol == -1 || refCol == -1 || titleCol > refCol {
		t.Errorf("ticket get --fields title,ref table header = %q, want TITLE before REF", tableOut)
	}

	jsonOut := captureStdout(t, func() {
		if err := runTicket([]string{"get", ref, "--url", apiURL, "--fields", "title,ref", "--json"}); err != nil {
			t.Fatalf("runTicket get --fields title,ref --json: %v", err)
		}
	})
	if strings.Index(jsonOut, `"ref"`) > strings.Index(jsonOut, `"title"`) {
		t.Errorf("ticket get --fields title,ref --json = %s, want ref before title (map keys sort alphabetically regardless of --fields order)", jsonOut)
	}
}

// TestTicketListFieldsProjectsExactKeySet is TestTicketGetFieldsProjectsExactKeySet's
// list-side counterpart.
func TestTicketListFieldsProjectsExactKeySet(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)
	t.Setenv("TICKETS_PROJECT", "ABC")

	out := captureStdout(t, func() {
		if err := runTicket([]string{"list", "--url", apiURL, "--fields", "ref,title", "--json"}); err != nil {
			t.Fatalf("runTicket list --fields: %v", err)
		}
	})
	var decoded struct {
		Tickets []map[string]any `json:"tickets"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("decode ticket list --fields --json output: %v (raw: %s)", err, out)
	}
	if len(decoded.Tickets) != 1 {
		t.Fatalf("ticket list --fields output has %d tickets, want 1", len(decoded.Tickets))
	}
	row := decoded.Tickets[0]
	if len(row) != 2 {
		t.Fatalf("ticket list --fields ref,title row has %d keys, want exactly 2 (%v)", len(row), row)
	}
	if row["ref"] != ref {
		t.Errorf("ticket list --fields output ref = %v, want %q", row["ref"], ref)
	}
}
