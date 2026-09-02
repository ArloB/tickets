// Package backup implements product spec §12's two distinct
// mechanisms: Backup/Restore (an online SQLite snapshot plus the
// managed blob store, for disaster recovery on the same machine) and
// Export/Import (a portable, redacted JSON document covering all
// non-secret domain data, for moving or archiving a project's content
// independent of any particular server installation). The two never
// share code beyond Manifest's checksum helpers — a restore replaces
// a data directory wholesale, an import inserts rows into whatever
// database `tickets import` is pointed at.
package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ArloB/tickets/internal/store"
)

// manifestFormatVersion is Manifest's own shape version, independent
// of the database schema version recorded alongside it — bumped only
// if Manifest's fields themselves change, not on every schema
// migration.
const manifestFormatVersion = 1

// Manifest is a backup directory's manifest.json: enough to verify
// every file's integrity before Restore touches active state, and to
// refuse restoring a backup newer than this build supports (the same
// rule Store.CheckCompatibility applies at ordinary startup).
type Manifest struct {
	FormatVersion int            `json:"format_version"`
	SchemaVersion int            `json:"schema_version"`
	ServerVersion string         `json:"server_version"`
	CreatedAt     string         `json:"created_at"`
	Files         []ManifestFile `json:"files"`
}

// ManifestFile is one file's identity within the backup directory —
// path is relative to the directory manifest.json itself lives in, so
// the manifest stays valid however the operator moves the backup
// around before restoring it.
type ManifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// sha256File hashes path's contents, streaming rather than reading it
// whole into memory — the database snapshot and attachment blobs can
// both be large (§9's streaming discipline applies here too).
func sha256File(path string) (sum string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("backup: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, fmt.Errorf("backup: hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// writeManifest marshals m as indented JSON to path.
func writeManifest(path string, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("backup: marshal manifest: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("backup: write manifest: %w", err)
	}
	return nil
}

// readManifest parses path's manifest.json.
func readManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("backup: read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("backup: parse manifest: %w", err)
	}
	return m, nil
}

func ValidateBackupDir(inputDir string) (Manifest, error) {
	manifest, err := readManifest(filepath.Join(inputDir, manifestFileName))
	if err != nil {
		return Manifest{}, err
	}
	if manifest.FormatVersion != manifestFormatVersion {
		return Manifest{}, fmt.Errorf(
			"restore: manifest format version %d is not supported by this build (want %d) — "+
				"restore with a compatible tickets version", manifest.FormatVersion, manifestFormatVersion)
	}
	highest, err := store.HighestEmbeddedMigrationVersion()
	if err != nil {
		return Manifest{}, fmt.Errorf("restore: schema version: %w", err)
	}
	if manifest.SchemaVersion > highest {
		return Manifest{}, fmt.Errorf(
			"restore: backup schema version %d is newer than this build supports (max %d) — "+
				"refusing to restore a backup taken by a newer tickets server",
			manifest.SchemaVersion, highest)
	}

	for _, f := range manifest.Files {
		sum, size, err := sha256File(filepath.Join(inputDir, f.Path))
		if err != nil {
			return Manifest{}, fmt.Errorf("restore: verify %s: %w", f.Path, err)
		}
		if sum != f.SHA256 || size != f.Size {
			return Manifest{}, fmt.Errorf(
				"restore: %s failed checksum verification (backup is corrupted or incomplete) — "+
					"active state left untouched", f.Path)
		}
	}
	return manifest, nil
}
