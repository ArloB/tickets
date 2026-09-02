package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ArloB/tickets/internal/backup"
	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/store"
)

func (s *Server) downloadExport(w http.ResponseWriter, r *http.Request) {
	st, err := store.Open(dataDir)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer func() { _ = st.Close() }()

	if r.URL.Query().Get("attachments") == "true" {
		s.downloadExportArchive(w, r, st)
		return
	}

	env, err := backup.Export(r.Context(), st.DB(), nil, "")
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="tickets-export.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(env); err != nil {
		writeError(w, r, err)
		return
	}
}

func (s *Server) downloadExportArchive(w http.ResponseWriter, r *http.Request, st *store.Store) {
	srcBlobs, err := blobstore.Open(dataDir)
	if err != nil {
		writeError(w, r, err)
		return
	}

	tmpDir, err := os.MkdirTemp("", "tickets-export-*")
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	env, err := backup.Export(r.Context(), st.DB(), srcBlobs, tmpDir)
	if err != nil {
		writeError(w, r, err)
		return
	}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "envelope.json"), data, 0o644); err != nil {
		writeError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="tickets-export.zip"`)
	if err := backup.WriteZip(w, tmpDir); err != nil {
		writeError(w, r, err)
		return
	}
}
