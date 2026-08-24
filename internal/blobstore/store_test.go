package blobstore

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPutOpenRoundTrip(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	content := "hello, attachments"
	hash, size, err := s.Put(strings.NewReader(content))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if size != int64(len(content)) {
		t.Errorf("size = %d, want %d", size, len(content))
	}
	sum := sha256.Sum256([]byte(content))
	if want := hex.EncodeToString(sum[:]); hash != want {
		t.Errorf("hash = %q, want %q", hash, want)
	}

	rc, err := s.Open(hash)
	if err != nil {
		t.Fatalf("Open blob: %v", err)
	}
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if string(got) != content {
		t.Errorf("blob content = %q, want %q", got, content)
	}
}

// TestPutDedup confirms writing identical bytes twice is a no-op the
// second time: same hash out, and only one file on disk under it.
func TestPutDedup(t *testing.T) {
	dataDir := t.TempDir()
	s, err := Open(dataDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	content := "duplicate me"
	hash1, _, err := s.Put(strings.NewReader(content))
	if err != nil {
		t.Fatalf("first Put: %v", err)
	}
	hash2, _, err := s.Put(strings.NewReader(content))
	if err != nil {
		t.Fatalf("second Put: %v", err)
	}
	if hash1 != hash2 {
		t.Fatalf("hash1 = %q, hash2 = %q, want equal", hash1, hash2)
	}

	shardDir := filepath.Join(dataDir, "blobs", hash1[:2])
	entries, err := os.ReadDir(shardDir)
	if err != nil {
		t.Fatalf("read shard dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("shard dir has %d entries, want exactly 1 (dedup)", len(entries))
	}
}

func TestPutShardsByHashPrefix(t *testing.T) {
	dataDir := t.TempDir()
	s, err := Open(dataDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	hash, _, err := s.Put(strings.NewReader("shard test"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	want := filepath.Join(dataDir, "blobs", hash[:2], hash)
	if _, err := os.Stat(want); err != nil {
		t.Errorf("blob not found at expected sharded path %s: %v", want, err)
	}
}

func TestOpenMissingBlob(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.Open(strings.Repeat("0", 64)); err == nil {
		t.Error("Open of a nonexistent blob returned nil error, want an error")
	}
}
