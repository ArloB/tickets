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

// TestOpenUnusualPath is internal/store.TestOpenUnusualPath's
// blobstore counterpart (Phase 6 Step 8's platform-testing drill, §15):
// a data directory whose path contains a space and a non-ASCII
// character must work here too, since the blobstore and the SQLite
// database share the same data directory in real deployments.
func TestOpenUnusualPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tickets tëst dir")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(%q): %v", dir, err)
	}

	content := "hello from an unusual path"
	hash, _, err := s.Put(strings.NewReader(content))
	if err != nil {
		t.Fatalf("Put: %v", err)
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

// TestHashesListsEveryStoredBlobNotTempFiles is Phase 6 Step 3's
// regression test for Hashes' Put-in-progress exclusion: an
// in-progress upload's "upload-*.tmp" file must never appear as if it
// were a committed blob, since integrity's orphan report would
// otherwise flag every concurrent upload as garbage.
func TestHashesListsEveryStoredBlobNotTempFiles(t *testing.T) {
	dataDir := t.TempDir()
	s, err := Open(dataDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	hashA, _, err := s.Put(strings.NewReader("blob a"))
	if err != nil {
		t.Fatalf("Put a: %v", err)
	}
	hashB, _, err := s.Put(strings.NewReader("blob b"))
	if err != nil {
		t.Fatalf("Put b: %v", err)
	}
	// Simulate a Put that's mid-flight when Hashes runs.
	tmp, err := os.CreateTemp(filepath.Join(dataDir, "blobs"), "upload-*.tmp")
	if err != nil {
		t.Fatalf("create in-progress temp file: %v", err)
	}
	_ = tmp.Close()

	hashes, err := s.Hashes()
	if err != nil {
		t.Fatalf("Hashes: %v", err)
	}
	got := map[string]bool{}
	for _, h := range hashes {
		got[h] = true
	}
	if len(got) != 2 || !got[hashA] || !got[hashB] {
		t.Fatalf("Hashes = %v, want exactly [%s %s]", hashes, hashA, hashB)
	}
}

// TestVerifyDetectsCorruptedBlob is Phase 6 Step 3's regression test
// for Verify: a blob whose on-disk bytes no longer hash to its own
// filename (bit rot, a tampered file) must be reported, and an intact
// blob must not be.
func TestVerifyDetectsCorruptedBlob(t *testing.T) {
	dataDir := t.TempDir()
	s, err := Open(dataDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	goodHash, _, err := s.Put(strings.NewReader("intact content"))
	if err != nil {
		t.Fatalf("Put good: %v", err)
	}
	badHash, _, err := s.Put(strings.NewReader("original content"))
	if err != nil {
		t.Fatalf("Put bad: %v", err)
	}
	// Corrupt it in place — the file's name no longer matches its bytes.
	if err := os.WriteFile(filepath.Join(dataDir, "blobs", badHash[:2], badHash), []byte("corrupted"), 0o600); err != nil {
		t.Fatalf("corrupt blob: %v", err)
	}

	results, err := s.Verify()
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	byHash := map[string]VerifyResult{}
	for _, r := range results {
		byHash[r.Hash] = r
	}
	if len(byHash) != 2 {
		t.Fatalf("Verify results = %+v, want exactly 2", results)
	}
	if byHash[goodHash].Err != nil {
		t.Errorf("intact blob reported an error: %v", byHash[goodHash].Err)
	}
	if byHash[badHash].Err == nil {
		t.Error("corrupted blob reported no error, want one")
	}
}

// TestRemoveDeletesBlob confirms Remove actually removes the file at
// a hash's on-disk path — the operation `admin integrity --gc` uses
// to prune a confirmed orphan.
func TestRemoveDeletesBlob(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	hash, _, err := s.Put(strings.NewReader("to be removed"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Remove(hash); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := s.Open(hash); err == nil {
		t.Error("blob still readable after Remove")
	}
}
