package backup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ArloB/tickets/internal/store"
)

// Restore replaces dataDir's database and blob store with inputDir's
// backup (`tickets admin restore`), verifying every manifest checksum
// before touching any active state — a corrupted or incomplete backup
// is refused with dataDir left exactly as it was.
//
// The server must not be running: under WAL another process holds the
// live database, and swapping tickets.db out from under it corrupts
// rather than recovers. force skips that precondition, for the one
// legitimate case it exists to guard against — a stale pidfile left by
// a crash (store.WritePidfile's doc comment explains why this check is
// presence, not a liveness probe, and so cannot tell that case apart
// from a genuinely running server on its own).
func Restore(ctx context.Context, dataDir, inputDir string, force bool) error {
	if !force {
		running, err := store.PidfileExists(dataDir)
		if err != nil {
			return fmt.Errorf("restore: check running server: %w", err)
		}
		if running {
			return fmt.Errorf(
				"restore: %s has a running server (tickets.pid present) — stop it first, "+
					"or pass --force if this is a stale pidfile left by a crash", dataDir)
		}
	}

	manifest, err := readManifest(filepath.Join(inputDir, manifestFileName))
	if err != nil {
		return err
	}
	if manifest.FormatVersion != manifestFormatVersion {
		return fmt.Errorf(
			"restore: manifest format version %d is not supported by this build (want %d) — "+
				"restore with a compatible tickets version", manifest.FormatVersion, manifestFormatVersion)
	}
	highest, err := store.HighestEmbeddedMigrationVersion()
	if err != nil {
		return fmt.Errorf("restore: schema version: %w", err)
	}
	if manifest.SchemaVersion > highest {
		return fmt.Errorf(
			"restore: backup schema version %d is newer than this build supports (max %d) — "+
				"refusing to restore a backup taken by a newer tickets server",
			manifest.SchemaVersion, highest)
	}

	for _, f := range manifest.Files {
		sum, size, err := sha256File(filepath.Join(inputDir, f.Path))
		if err != nil {
			return fmt.Errorf("restore: verify %s: %w", f.Path, err)
		}
		if sum != f.SHA256 || size != f.Size {
			return fmt.Errorf(
				"restore: %s failed checksum verification (backup is corrupted or incomplete) — "+
					"active state left untouched", f.Path)
		}
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("restore: create data directory: %w", err)
	}

	// Swap the database in via copy-to-temp-then-rename within dataDir
	// itself, so a crash mid-copy never leaves a half-written
	// tickets.db in place, and rename stays atomic (same filesystem)
	// even though inputDir may be on a different one than dataDir.
	if err := swapFile(filepath.Join(inputDir, dbFileName), filepath.Join(dataDir, dbFileName)); err != nil {
		return err
	}
	// A stale -wal/-shm from the *previous* database must not survive
	// the swap: VACUUM INTO's output is a complete, self-contained
	// file, and replaying an old WAL against the newly restored file
	// would corrupt it rather than recover it.
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(filepath.Join(dataDir, dbFileName+suffix)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("restore: remove stale %s: %w", dbFileName+suffix, err)
		}
	}

	// The blob store is replaced wholesale, not merged: a restore
	// reproduces the backup's state exactly. A post-backup blob left
	// behind by a merge would be a harmless orphan (ADR 0007) but not
	// part of the reference installation this restore reproduces.
	blobsDir := filepath.Join(dataDir, "blobs")
	if err := os.RemoveAll(blobsDir); err != nil {
		return fmt.Errorf("restore: clear existing blob store: %w", err)
	}
	srcBlobsDir := filepath.Join(inputDir, "blobs")
	if _, err := os.Stat(srcBlobsDir); err == nil {
		if err := copyDir(srcBlobsDir, blobsDir); err != nil {
			return fmt.Errorf("restore: copy blob store: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("restore: stat backup blob store: %w", err)
	} else if err := os.MkdirAll(blobsDir, 0o755); err != nil {
		return fmt.Errorf("restore: create blob store: %w", err)
	}

	// Open-then-close as a final integrity check: an old-schema backup
	// migrates forward exactly as it would on a normal server startup
	// (Step 3's pre-migration backup applies here too), and a database
	// that fails to open at all is caught immediately rather than
	// discovered on the next `tickets server` start.
	st, err := store.Open(dataDir)
	if err != nil {
		return fmt.Errorf("restore: restored database failed to open: %w", err)
	}
	return st.Close()
}

func swapFile(srcPath, destPath string) error {
	tmp := destPath + ".tmp"
	if err := copyFile(srcPath, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, destPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("restore: swap in %s: %w", destPath, err)
	}
	return nil
}

func copyFile(srcPath, destPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("restore: open %s: %w", srcPath, err)
	}
	defer func() { _ = src.Close() }()

	dest, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("restore: create %s: %w", destPath, err)
	}
	defer func() { _ = dest.Close() }()

	if _, err := io.Copy(dest, src); err != nil {
		return fmt.Errorf("restore: copy %s: %w", srcPath, err)
	}
	return nil
}

func copyDir(srcDir, destDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}
