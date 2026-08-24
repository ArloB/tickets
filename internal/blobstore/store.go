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
