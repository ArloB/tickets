package httpapi

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ArloB/tickets/internal/backup"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

var maxAdminUploadSize int64 = 10 << 30

func SetMaxAdminUploadBytes(n int64) {
	maxAdminUploadSize = n
}

const PendingRestoreDirName = ".pending-restore"
const FailedRestoreDirName = ".pending-restore.failed"
const RestoreErrorFileName = ".pending-restore-error.txt"

type restoreStagedResponse struct {
	Staged bool `json:"staged"`
}

func (s *Server) uploadRestore(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminUploadSize)

	tmpFile, err := os.CreateTemp("", "tickets-restore-upload-*.zip")
	if err != nil {
		writeError(w, r, err)
		return
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := io.Copy(tmpFile, r.Body); err != nil {
		_ = tmpFile.Close()
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, r, &service.Error{Code: domain.ErrUploadTooLarge, Message: "upload exceeds the configured size limit"})
			return
		}
		writeError(w, r, err)
		return
	}
	if err := tmpFile.Close(); err != nil {
		writeError(w, r, err)
		return
	}

	extractDir := filepath.Join(dataDir, PendingRestoreDirName+".tmp")
	_ = os.RemoveAll(extractDir)
	if err := backup.ExtractZip(tmpPath, extractDir); err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "not a valid backup archive: " + err.Error()})
		return
	}
	if _, err := backup.ValidateBackupDir(extractDir); err != nil {
		_ = os.RemoveAll(extractDir)
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: err.Error()})
		return
	}

	pendingDir := filepath.Join(dataDir, PendingRestoreDirName)
	if err := os.RemoveAll(pendingDir); err != nil {
		_ = os.RemoveAll(extractDir)
		writeError(w, r, err)
		return
	}
	if err := os.Rename(extractDir, pendingDir); err != nil {
		_ = os.RemoveAll(extractDir)
		writeError(w, r, err)
		return
	}
	_ = os.RemoveAll(filepath.Join(dataDir, FailedRestoreDirName))
	_ = os.Remove(filepath.Join(dataDir, RestoreErrorFileName))

	writeJSON(w, http.StatusOK, restoreStagedResponse{Staged: true})
}

func (s *Server) restoreStatus(w http.ResponseWriter, r *http.Request) {
	status := restorePendingStatus{}
	if _, err := os.Stat(filepath.Join(dataDir, PendingRestoreDirName)); err == nil {
		status.Pending = true
	} else if !os.IsNotExist(err) {
		writeError(w, r, err)
		return
	}
	if data, err := os.ReadFile(filepath.Join(dataDir, RestoreErrorFileName)); err == nil {
		status.Failed = true
		status.LastError = string(data)
	} else if !os.IsNotExist(err) {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

type restorePendingStatus struct {
	Pending   bool   `json:"pending"`
	Failed    bool   `json:"failed"`
	LastError string `json:"last_error,omitempty"`
}

func (s *Server) dismissFailedRestore(w http.ResponseWriter, r *http.Request) {
	if err := os.RemoveAll(filepath.Join(dataDir, FailedRestoreDirName)); err != nil {
		writeError(w, r, err)
		return
	}
	if err := os.Remove(filepath.Join(dataDir, RestoreErrorFileName)); err != nil && !os.IsNotExist(err) {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
