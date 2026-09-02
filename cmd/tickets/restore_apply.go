package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ArloB/tickets/internal/backup"
	"github.com/ArloB/tickets/internal/httpapi"
)

func applyPendingRestore(ctx context.Context, dataDir string, logger *slog.Logger) {
	pendingDir := filepath.Join(dataDir, httpapi.PendingRestoreDirName)
	if _, err := os.Stat(pendingDir); err != nil {
		return
	}

	logger.Info("applying staged restore", "data_dir", dataDir)
	if err := backup.Restore(ctx, dataDir, pendingDir, true); err != nil {
		logger.Error("staged restore failed; starting with pre-restore data instead", "error", err)
		failedDir := filepath.Join(dataDir, httpapi.FailedRestoreDirName)
		_ = os.RemoveAll(failedDir)
		_ = os.Rename(pendingDir, failedDir)
		_ = os.WriteFile(filepath.Join(dataDir, httpapi.RestoreErrorFileName), []byte(err.Error()), 0o644)
		return
	}

	_ = os.RemoveAll(pendingDir)
	logger.Info("staged restore applied")
}
