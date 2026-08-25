package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/buildinfo"
	"github.com/ArloB/tickets/internal/store"
)

const dbFileName = "tickets.db"
const manifestFileName = "manifest.json"

// Backup writes a portable, self-contained snapshot of dataDir's
// database and blob store to outputDir (`tickets admin backup`,
// product spec §12's online-backup mechanism — distinct from Step 3's
// pre-migration safety net under <data-dir>/backups/, which this never
// touches since it snapshots via VACUUM INTO rather than copying
// dataDir's raw files, and from Export's redacted portable JSON).
// outputDir must not already exist: Backup creates it and refuses to
// merge into or overwrite one that does, so a stale manifest from an
// earlier backup can never mix with a new one.
//
// The database snapshot is taken *before* the blob store is copied.
// Blobs are append-only outside `admin integrity --gc` (ADR 0007), so
// every file_hash the snapshot references at the moment VACUUM INTO
// runs is still on disk once the blob copy that follows it finishes —
// reversing the order would let an attachment committed in between
// reference a blob this backup never copied.
func Backup(ctx context.Context, dataDir, outputDir string) (Manifest, error) {
	if _, err := os.Stat(outputDir); err == nil {
		return Manifest{}, fmt.Errorf("backup: output directory %s already exists", outputDir)
	} else if !os.IsNotExist(err) {
		return Manifest{}, fmt.Errorf("backup: stat output directory: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return Manifest{}, fmt.Errorf("backup: create output directory: %w", err)
	}

	st, err := store.Open(dataDir)
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	schemaVersion, err := store.HighestEmbeddedMigrationVersion()
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: schema version: %w", err)
	}

	dbPath := filepath.Join(outputDir, dbFileName)
	if _, err := st.DB().ExecContext(ctx, `VACUUM INTO ?`, dbPath); err != nil {
		return Manifest{}, fmt.Errorf("backup: snapshot database: %w", err)
	}
	dbSum, dbSize, err := sha256File(dbPath)
	if err != nil {
		return Manifest{}, err
	}

	manifest := Manifest{
		FormatVersion: manifestFormatVersion,
		SchemaVersion: schemaVersion,
		ServerVersion: buildinfo.Version,
		CreatedAt:     time.Now().UTC().Format(store.TimeLayout),
		Files:         []ManifestFile{{Path: dbFileName, SHA256: dbSum, Size: dbSize}},
	}

	srcBlobs, err := blobstore.Open(dataDir)
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: open source blob store: %w", err)
	}
	// blobstore.Open creates outputDir/blobs, the same sharded layout
	// a real data directory has — so restore can move this directory
	// tree into place verbatim.
	destBlobs, err := blobstore.Open(outputDir)
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: open destination blob store: %w", err)
	}
	hashes, err := srcBlobs.Hashes()
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: list blobs: %w", err)
	}
	for _, hash := range hashes {
		file, err := copyBlob(srcBlobs, destBlobs, hash)
		if err != nil {
			return Manifest{}, err
		}
		manifest.Files = append(manifest.Files, file)
	}

	if err := writeManifest(filepath.Join(outputDir, manifestFileName), manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// copyBlob streams one blob from src into dest, re-hashing as it
// writes — Put's own content-address check, doubling as this backup's
// integrity proof for the copy rather than trusting the source
// filename. relPath is reconstructed from the returned hash so the
// manifest names the blob's actual on-disk location under outputDir
// (dest and src use the identical sharding scheme, blobstore.Store's
// only implementation of it).
func copyBlob(src, dest *blobstore.Store, hash string) (ManifestFile, error) {
	r, err := src.Open(hash)
	if err != nil {
		return ManifestFile{}, fmt.Errorf("backup: open blob %s: %w", hash, err)
	}
	defer func() { _ = r.Close() }()

	sum, size, err := dest.Put(r)
	if err != nil {
		return ManifestFile{}, fmt.Errorf("backup: copy blob %s: %w", hash, err)
	}
	if sum != hash {
		return ManifestFile{}, fmt.Errorf("backup: blob %s re-hashed to %s on copy — source is corrupted", hash, sum)
	}
	shard := hash
	if len(shard) > 2 {
		shard = hash[:2]
	}
	relPath := filepath.ToSlash(filepath.Join("blobs", shard, hash))
	return ManifestFile{Path: relPath, SHA256: sum, Size: size}, nil
}
