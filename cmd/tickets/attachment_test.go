package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestAttachmentUploadListGetDownloadDeleteJSON exercises the full
// CLI surface end-to-end against a real running server: upload a
// file, list it, get its metadata, download the bytes back, replace
// with a new version, then delete.
func TestAttachmentUploadListGetDownloadDeleteJSON(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	srcPath := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(srcPath, []byte("hello from the CLI"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	uploadOut := captureStdout(t, func() {
		if err := runAttachment([]string{"upload", ref, srcPath, "--url", apiURL, "--title", "cli notes", "--json"}); err != nil {
			t.Fatalf("attachment upload: %v", err)
		}
	})
	var uploaded map[string]any
	if err := json.Unmarshal([]byte(uploadOut), &uploaded); err != nil {
		t.Fatalf("decode upload output: %v (raw: %s)", err, uploadOut)
	}
	id := uploaded["id"].(float64)
	if uploaded["file_name"] != "notes.txt" {
		t.Errorf("file_name = %v, want notes.txt", uploaded["file_name"])
	}
	if uploaded["current_version"].(float64) != 1 {
		t.Errorf("current_version = %v, want 1", uploaded["current_version"])
	}

	// --- list ---
	listOut := captureStdout(t, func() {
		if err := runAttachment([]string{"list", ref, "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("attachment list: %v", err)
		}
	})
	var list struct {
		Attachments []map[string]any `json:"attachments"`
	}
	if err := json.Unmarshal([]byte(listOut), &list); err != nil {
		t.Fatalf("decode list output: %v (raw: %s)", err, listOut)
	}
	if len(list.Attachments) != 1 {
		t.Fatalf("listed %d attachments, want 1", len(list.Attachments))
	}

	// --- download ---
	idStr := formatFloatID(id)
	destPath := filepath.Join(t.TempDir(), "downloaded.txt")
	if err := runAttachment([]string{"download", idStr, "--url", apiURL, "--output", destPath}); err != nil {
		t.Fatalf("attachment download: %v", err)
	}
	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != "hello from the CLI" {
		t.Errorf("downloaded content = %q, want %q", got, "hello from the CLI")
	}

	// --- replace ---
	revisedPath := filepath.Join(t.TempDir(), "notes-v2.txt")
	if err := os.WriteFile(revisedPath, []byte("revised via CLI"), 0o644); err != nil {
		t.Fatalf("write revised file: %v", err)
	}
	replaceOut := captureStdout(t, func() {
		if err := runAttachment([]string{"replace", idStr, revisedPath, "--url", apiURL, "--if-version", "1", "--json"}); err != nil {
			t.Fatalf("attachment replace: %v", err)
		}
	})
	var replaced map[string]any
	if err := json.Unmarshal([]byte(replaceOut), &replaced); err != nil {
		t.Fatalf("decode replace output: %v (raw: %s)", err, replaceOut)
	}
	if replaced["current_version"].(float64) != 2 {
		t.Errorf("current_version after replace = %v, want 2", replaced["current_version"])
	}

	// --- delete ---
	if err := runAttachment([]string{"delete", idStr, "--url", apiURL, "--if-version", "2"}); err != nil {
		t.Fatalf("attachment delete: %v", err)
	}
	if err := runAttachment([]string{"get", idStr, "--url", apiURL, "--json"}); err == nil {
		t.Error("attachment get after delete: want error, got nil")
	}
}

// TestAttachmentPathCreate covers the path-reference variant end to
// end over the CLI, including that its metadata round-trips correctly
// (not that anything reads the path — that's httpapi's
// TestAttachmentPathReferenceNeverRead).
func TestAttachmentPathCreate(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	out := captureStdout(t, func() {
		if err := runAttachment([]string{"path", ref, "/var/data/design.pdf", "--url", apiURL, "--title", "design doc", "--json"}); err != nil {
			t.Fatalf("attachment path: %v", err)
		}
	})
	var attachment map[string]any
	if err := json.Unmarshal([]byte(out), &attachment); err != nil {
		t.Fatalf("decode path output: %v (raw: %s)", err, out)
	}
	if attachment["kind"] != "path" {
		t.Errorf("kind = %v, want path", attachment["kind"])
	}
	if attachment["path_value"] != "/var/data/design.pdf" {
		t.Errorf("path_value = %v, want /var/data/design.pdf", attachment["path_value"])
	}
}

// TestAttachmentDeleteRequiresIfVersion mirrors
// TestTicketUpdateRequiresIfVersion's shape for the attachment delete
// subcommand.
func TestAttachmentDeleteRequiresIfVersion(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, ref := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	srcPath := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(srcPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	out := captureStdout(t, func() {
		if err := runAttachment([]string{"upload", ref, srcPath, "--url", apiURL, "--title", "t", "--json"}); err != nil {
			t.Fatalf("attachment upload: %v", err)
		}
	})
	var uploaded map[string]any
	if err := json.Unmarshal([]byte(out), &uploaded); err != nil {
		t.Fatalf("decode upload output: %v", err)
	}
	idStr := formatFloatID(uploaded["id"].(float64))

	if err := runAttachment([]string{"delete", idStr, "--url", apiURL}); err == nil {
		t.Error("attachment delete with no --if-version: want error, got nil")
	}
}

func formatFloatID(f float64) string {
	return strconv.FormatInt(int64(f), 10)
}
