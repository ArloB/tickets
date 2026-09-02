package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ArloB/tickets/internal/backup"
	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
)

func (s *Server) setupImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminUploadSize)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, r, &service.Error{Code: domain.ErrUploadTooLarge, Message: "upload exceeds the configured size limit"})
			return
		}
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "invalid multipart request: " + err.Error()})
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	envelopeHeaders := r.MultipartForm.File["envelope"]
	if len(envelopeHeaders) != 1 {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Field: "envelope", Message: `exactly one file part named "envelope" is required`})
		return
	}
	envelopeFile, err := envelopeHeaders[0].Open()
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read envelope file"})
		return
	}
	defer func() { _ = envelopeFile.Close() }()

	var env backup.Envelope
	if err := json.NewDecoder(envelopeFile).Decode(&env); err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "invalid export envelope JSON: " + err.Error()})
		return
	}

	attachmentsDir := ""
	if attachmentHeaders := r.MultipartForm.File["attachments"]; len(attachmentHeaders) == 1 {
		f, err := attachmentHeaders[0].Open()
		if err != nil {
			writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read attachments file"})
			return
		}
		defer func() { _ = f.Close() }()

		tmpZip, err := os.CreateTemp("", "tickets-import-attachments-*.zip")
		if err != nil {
			writeError(w, r, err)
			return
		}
		tmpZipPath := tmpZip.Name()
		defer func() { _ = os.Remove(tmpZipPath) }()
		if _, err := io.Copy(tmpZip, f); err != nil {
			_ = tmpZip.Close()
			writeError(w, r, err)
			return
		}
		if err := tmpZip.Close(); err != nil {
			writeError(w, r, err)
			return
		}

		attachmentsDir = filepath.Join(dataDir, ".import-attachments.tmp")
		_ = os.RemoveAll(attachmentsDir)
		if err := backup.ExtractZip(tmpZipPath, attachmentsDir); err != nil {
			writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "not a valid attachments archive: " + err.Error()})
			return
		}
		defer func() { _ = os.RemoveAll(attachmentsDir) }()
	}

	st, err := store.Open(dataDir)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer func() { _ = st.Close() }()

	var dstBlobs *blobstore.Store
	if attachmentsDir != "" {
		dstBlobs, err = blobstore.Open(dataDir)
		if err != nil {
			writeError(w, r, err)
			return
		}
	}

	report, err := backup.Import(r.Context(), st.DB(), env, attachmentsDir, dstBlobs, true)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}
