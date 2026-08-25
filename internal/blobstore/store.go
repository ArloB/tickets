// Package blobstore is the content-addressed file store ADR 0007
// fixes for uploaded attachments: bytes are named by their SHA-256
// hash and written under the configured data directory, sharded by
// the hash's first two hex characters so a single directory never
// holds every blob in the store. Deduplication falls directly out of
// this naming scheme — writing the same bytes twice produces the same
// path, so the second write is a no-op.
//
// Path attachments never enter this package: ADR 0007 is explicit
// that there is no code path that opens a path attachment's target,
// so internal/service never calls Put for one.
package blobstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store is a content-addressed blob store rooted at a directory under
// the server's configured data directory.
type Store struct {
	root string
}

// Open prepares a blobstore under dataDir/blobs, creating it if
// necessary. Mirrors store.Open's own MkdirAll-on-open convention.
func Open(dataDir string) (*Store, error) {
	root := filepath.Join(dataDir, "blobs")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("blobstore: create root: %w", err)
	}
	return &Store{root: root}, nil
}

// Put streams r to a temporary file while hashing it, then renames the
// temp file into place at its content-addressed path — streamed the
// whole way, never buffered whole in memory, per §9. If a blob with
// the same hash already exists, the temp file is discarded instead of
// replacing it (dedup).
func (s *Store) Put(r io.Reader) (hash string, size int64, err error) {
	tmp, err := os.CreateTemp(s.root, "upload-*.tmp")
	if err != nil {
		return "", 0, fmt.Errorf("blobstore: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op once successfully renamed

	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(tmp, h), r)
	closeErr := tmp.Close()
	if copyErr != nil {
		return "", 0, fmt.Errorf("blobstore: write blob: %w", copyErr)
	}
	if closeErr != nil {
		return "", 0, fmt.Errorf("blobstore: close temp file: %w", closeErr)
	}

	sum := hex.EncodeToString(h.Sum(nil))
	dest := s.path(sum)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", 0, fmt.Errorf("blobstore: create shard dir: %w", err)
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		return sum, n, nil // already stored under this hash
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", 0, fmt.Errorf("blobstore: stat destination: %w", statErr)
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return "", 0, fmt.Errorf("blobstore: rename into place: %w", err)
	}
	return sum, n, nil
}

// Open returns a streamed reader for the blob named by hash. Callers
// must Close it.
func (s *Store) Open(hash string) (io.ReadCloser, error) {
	f, err := os.Open(s.path(hash))
	if err != nil {
		return nil, fmt.Errorf("blobstore: open blob %s: %w", hash, err)
	}
	return f, nil
}

// path computes a blob's sharded on-disk location. hash is always a
// server-computed SHA-256 hex digest (from Put, or read back verbatim
// out of attachment_versions.file_hash) — never taken directly from a
// request — so this does not need to defend against path traversal in
// its input.
func (s *Store) path(hash string) string {
	if len(hash) < 2 {
		return filepath.Join(s.root, hash)
	}
	return filepath.Join(s.root, hash[:2], hash)
}

// Hashes lists every blob currently on disk, by walking the sharded
// directory tree — the on-disk inventory `tickets admin integrity`
// (Phase 6 Step 3) cross-references against every file_hash/checksum
// referenced from attachments/attachment_versions/content_items/
// content_versions to find orphans (ADR 0007's open item: a blob
// written just before its enclosing transaction rolled back has no
// referencing row and is never cleaned up automatically).
func (s *Store) Hashes() ([]string, error) {
	var hashes []string
	err := filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), "upload-") && strings.HasSuffix(d.Name(), ".tmp") {
			return nil // an in-progress Put's temp file, not a committed blob
		}
		hashes = append(hashes, d.Name())
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("blobstore: walk blobs: %w", err)
	}
	return hashes, nil
}

// VerifyResult is one stored blob's integrity check outcome —
// `tickets admin integrity`'s per-blob finding.
type VerifyResult struct {
	Hash string
	// Err is nil when the blob's actual content hashes to Hash — a
	// content-addressed store's own name for a file IS its declared
	// checksum, so no separate stored checksum is needed to detect
	// corruption, only a re-hash.
	Err error
}

// Verify re-hashes every blob Hashes lists and reports any whose
// content no longer matches its filename — bit rot or an on-disk
// tamper, not something an application-level bug could produce, since
// nothing in this codebase ever writes to a blob's path a second time
// (Put's rename-into-place is the only writer, and it skips an
// already-existing destination).
func (s *Store) Verify() ([]VerifyResult, error) {
	hashes, err := s.Hashes()
	if err != nil {
		return nil, err
	}
	results := make([]VerifyResult, 0, len(hashes))
	for _, hash := range hashes {
		results = append(results, VerifyResult{Hash: hash, Err: s.verifyOne(hash)})
	}
	return results, nil
}

func (s *Store) verifyOne(hash string) error {
	f, err := os.Open(s.path(hash))
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("read: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != hash {
		return fmt.Errorf("content hashes to %s, filename claims %s", got, hash)
	}
	return nil
}

// ModTime returns the on-disk modification time of the blob named by
// hash — `tickets admin integrity --gc` uses this to skip
// recently-written blobs, since CreateAttachment's blobstore.Put
// happens before its enclosing internal/service transaction commits
// (this package's doc comment, ADR 0007's Consequences): a blob that's
// merely mid-upload, not yet referenced because its owning row hasn't
// committed, looks identical to a genuine orphan for the seconds
// between Put and commit.
func (s *Store) ModTime(hash string) (time.Time, error) {
	info, err := os.Stat(s.path(hash))
	if err != nil {
		return time.Time{}, fmt.Errorf("blobstore: stat blob %s: %w", hash, err)
	}
	return info.ModTime(), nil
}

// Remove deletes one blob by hash — `tickets admin integrity --gc`'s
// only write operation, and the first thing in this package that ever
// deletes a blob (content-addressing gives every other caller a
// reason never to: the same bytes uploaded again just dedups onto
// whatever's already there). hash must already be confirmed orphaned
// by the caller; this does not re-check.
func (s *Store) Remove(hash string) error {
	if err := os.Remove(s.path(hash)); err != nil {
		return fmt.Errorf("blobstore: remove blob %s: %w", hash, err)
	}
	return nil
}
