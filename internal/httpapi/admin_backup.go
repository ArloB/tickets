package httpapi

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/ArloB/tickets/internal/backup"
)

var dataDir string

func SetDataDir(dir string) {
	dataDir = dir
}

func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request) {
	tmpDir, err := os.MkdirTemp("", "tickets-backup-*")
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	outputDir := filepath.Join(tmpDir, "backup")
	if _, err := backup.Backup(r.Context(), dataDir, outputDir); err != nil {
		writeError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="tickets-backup.zip"`)
	if err := backup.WriteZip(w, outputDir); err != nil {
		writeError(w, r, err)
		return
	}
}
