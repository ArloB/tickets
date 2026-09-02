package httpapi

import (
	"io"
	"net/http"

	"github.com/ArloB/tickets/internal/backup"
	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
)

func (s *Server) buildIntegrityReport(r *http.Request, gc bool) (backup.IntegrityReport, error) {
	st, err := store.Open(dataDir)
	if err != nil {
		return backup.IntegrityReport{}, err
	}
	defer func() { _ = st.Close() }()

	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		return backup.IntegrityReport{}, err
	}

	return backup.BuildIntegrityReport(r.Context(), st, blobs, gc)
}

func (s *Server) getIntegrityReport(w http.ResponseWriter, r *http.Request) {
	report, err := s.buildIntegrityReport(r, false)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

type gcRequest struct {
	Confirm bool `json:"confirm"`
}

func (s *Server) runGC(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req gcRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	if !req.Confirm {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Field: "confirm", Message: "confirm must be true to remove orphaned blobs"})
		return
	}

	report, err := s.buildIntegrityReport(r, true)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}
