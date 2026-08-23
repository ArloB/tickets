package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCommentAddInlineAndJSON(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	out := captureStdout(t, func() {
		if err := runComment([]string{"add", ref, "--url", apiURL, "--body", "Looking into this", "--json"}); err != nil {
			t.Fatalf("runComment add: %v", err)
		}
	})
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("decode comment add --json output: %v (raw: %s)", err, out)
	}
	if decoded["body"] != "Looking into this" {
		t.Errorf("comment add output body = %v, want %q", decoded["body"], "Looking into this")
	}
}

// TestCommentAddIdempotencyKeyIsWired proves --idempotency-key
// actually reaches apiclient.CreateComment, not just that the flag
// parses: replaying the same key with identical arguments must return
// the original comment's id, not create a second comment.
func TestCommentAddIdempotencyKeyIsWired(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	runAdd := func() map[string]any {
		out := captureStdout(t, func() {
			if err := runComment([]string{
				"add", ref, "--url", apiURL, "--body", "Looking into this",
				"--idempotency-key", "dup-key-1", "--json",
			}); err != nil {
				t.Fatalf("runComment add: %v", err)
			}
		})
		var m map[string]any
		if err := json.Unmarshal([]byte(out), &m); err != nil {
			t.Fatalf("decode comment add --json output: %v (raw: %s)", err, out)
		}
		return m
	}

	first := runAdd()
	replay := runAdd()
	if first["id"] == nil || first["id"] != replay["id"] {
		t.Errorf("comment add replayed with the same --idempotency-key: ids = %v, %v — want the same id (the flag isn't reaching the server)", first["id"], replay["id"])
	}
}

func TestCommentAddRequiresExactlyOneOfBodyOrBodyFile(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runComment([]string{"add", ref, "--url", apiURL}); err == nil {
		t.Error("comment add with neither --body nor --body-file: want error, got nil")
	}
	if err := runComment([]string{"add", ref, "--url", apiURL, "--body", "x", "--body-file", "-"}); err == nil {
		t.Error("comment add with both --body and --body-file: want error, got nil")
	}
}

func TestCommentAddRequiresLeadingRef(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runComment([]string{"add", "--url", apiURL, "--body", "x"}); err == nil {
		t.Error("comment add with no leading ref: want error, got nil")
	}
}

func TestCommentListEditDeleteLifecycle(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	captureStdout(t, func() {
		if err := runComment([]string{"add", ref, "--url", apiURL, "--body", "First pass"}); err != nil {
			t.Fatalf("runComment add: %v", err)
		}
	})

	listOut := captureStdout(t, func() {
		if err := runComment([]string{"list", ref, "--url", apiURL}); err != nil {
			t.Fatalf("runComment list: %v", err)
		}
	})
	if !strings.Contains(listOut, "First pass") {
		t.Errorf("comment list output = %q, want it to contain the comment body", listOut)
	}

	editOut := captureStdout(t, func() {
		if err := runComment([]string{"edit", "1", "--url", apiURL, "--body", "Edited", "--if-version", "1", "--json"}); err != nil {
			t.Fatalf("runComment edit: %v", err)
		}
	})
	var edited map[string]any
	if err := json.Unmarshal([]byte(editOut), &edited); err != nil {
		t.Fatalf("decode comment edit --json output: %v (raw: %s)", err, editOut)
	}
	if edited["body"] != "Edited" {
		t.Errorf("comment edit output body = %v, want %q", edited["body"], "Edited")
	}

	deleteOut := captureStdout(t, func() {
		if err := runComment([]string{"delete", "1", "--url", apiURL, "--if-version", "2"}); err != nil {
			t.Fatalf("runComment delete: %v", err)
		}
	})
	if !strings.Contains(deleteOut, "deleted") {
		t.Errorf("comment delete output = %q, want it to confirm deletion", deleteOut)
	}
}

func TestCommentEditDeleteRequireIfVersion(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runComment([]string{"edit", "1", "--url", apiURL, "--body", "x"}); err == nil {
		t.Error("comment edit with no --if-version: want error, got nil")
	}
	if err := runComment([]string{"delete", "1", "--url", apiURL}); err == nil {
		t.Error("comment delete with no --if-version: want error, got nil")
	}
}

func TestCommentRequiresSubcommand(t *testing.T) {
	if err := runComment(nil); err == nil {
		t.Error("runComment with no subcommand: want error, got nil")
	}
}

func TestCommentRejectsUnknownSubcommand(t *testing.T) {
	if err := runComment([]string{"not-a-real-subcommand"}); err == nil {
		t.Error("runComment with an unknown subcommand: want error, got nil")
	}
}
