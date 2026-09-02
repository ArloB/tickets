package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ArloB/tickets/internal/backup"
	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
)

func TestDownloadBackupRequiresAdmin(t *testing.T) {
	ts := newTestServer(t)

	createResp, _ := ts.do(http.MethodPost, "/accounts", nil,
		mustJSON(t, map[string]any{"username": "bob", "password": "bobs-password-here"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create bob's account status = %d", createResp.StatusCode)
	}
	bobSession, bobCSRF := loginAs(t, ts, "bob", "bobs-password-here")

	req, err := http.NewRequest(http.MethodGet, ts.url+"/api/v1/admin/backup", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: bobSession})
	req.Header.Set("X-CSRF-Token", bobCSRF)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin download backup status = %d, want 403", resp.StatusCode)
	}
}

func TestDownloadBackupProducesValidZip(t *testing.T) {
	ts := newTestServer(t)

	resp, body := ts.do(http.MethodGet, "/admin/backup", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download backup status = %d, body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", ct)
	}

	zipPath := filepath.Join(t.TempDir(), "backup.zip")
	if err := os.WriteFile(zipPath, body, 0o644); err != nil {
		t.Fatalf("write zip: %v", err)
	}
	extractDir := filepath.Join(t.TempDir(), "extracted")
	if err := backup.ExtractZip(zipPath, extractDir); err != nil {
		t.Fatalf("ExtractZip: %v", err)
	}
	if _, err := backup.ValidateBackupDir(extractDir); err != nil {
		t.Errorf("ValidateBackupDir on downloaded backup: %v", err)
	}
}

func TestRestoreUploadStagesThenStatusReflectsIt(t *testing.T) {
	ts := newTestServer(t)

	backupResp, backupBody := ts.do(http.MethodGet, "/admin/backup", nil, nil)
	if backupResp.StatusCode != http.StatusOK {
		t.Fatalf("download backup status = %d", backupResp.StatusCode)
	}

	req, err := http.NewRequest(http.MethodPost, ts.url+"/api/v1/admin/restore", bytes.NewReader(backupBody))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/zip")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ts.sessionID})
	req.Header.Set("X-CSRF-Token", ts.csrfToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload restore: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload restore status = %d", resp.StatusCode)
	}

	statusResp, statusBody := ts.do(http.MethodGet, "/admin/restore", nil, nil)
	if statusResp.StatusCode != http.StatusOK {
		t.Fatalf("restore status = %d", statusResp.StatusCode)
	}
	var status restorePendingStatus
	if err := json.Unmarshal(statusBody, &status); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if !status.Pending {
		t.Error("Pending = false, want true after a staged restore upload")
	}
	if status.Failed {
		t.Error("Failed = true, want false right after a successful stage")
	}

	if _, err := os.Stat(filepath.Join(dataDir, PendingRestoreDirName)); err != nil {
		t.Errorf("pending restore directory not created: %v", err)
	}
}

func TestRestoreUploadRejectsInvalidArchive(t *testing.T) {
	ts := newTestServer(t)

	req, err := http.NewRequest(http.MethodPost, ts.url+"/api/v1/admin/restore", bytes.NewReader([]byte("not a zip file")))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/zip")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ts.sessionID})
	req.Header.Set("X-CSRF-Token", ts.csrfToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload restore: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("upload invalid archive status = %d, want 400", resp.StatusCode)
	}

	statusResp, statusBody := ts.do(http.MethodGet, "/admin/restore", nil, nil)
	if statusResp.StatusCode != http.StatusOK {
		t.Fatalf("restore status = %d", statusResp.StatusCode)
	}
	var status restorePendingStatus
	if err := json.Unmarshal(statusBody, &status); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if status.Pending {
		t.Error("Pending = true after a rejected upload, want false")
	}
}

func TestDismissFailedRestore(t *testing.T) {
	ts := newTestServer(t)

	failedDir := filepath.Join(dataDir, FailedRestoreDirName)
	if err := os.MkdirAll(failedDir, 0o755); err != nil {
		t.Fatalf("create failed dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, RestoreErrorFileName), []byte("boom"), 0o644); err != nil {
		t.Fatalf("write error file: %v", err)
	}

	statusResp, statusBody := ts.do(http.MethodGet, "/admin/restore", nil, nil)
	if statusResp.StatusCode != http.StatusOK {
		t.Fatalf("restore status = %d", statusResp.StatusCode)
	}
	var status restorePendingStatus
	if err := json.Unmarshal(statusBody, &status); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if !status.Failed || status.LastError != "boom" {
		t.Fatalf("status = %+v, want Failed=true LastError=boom", status)
	}

	dismissResp, _ := ts.do(http.MethodDelete, "/admin/restore", nil, nil)
	if dismissResp.StatusCode != http.StatusNoContent {
		t.Fatalf("dismiss status = %d, want 204", dismissResp.StatusCode)
	}

	statusResp2, statusBody2 := ts.do(http.MethodGet, "/admin/restore", nil, nil)
	if statusResp2.StatusCode != http.StatusOK {
		t.Fatalf("restore status = %d", statusResp2.StatusCode)
	}
	var status2 restorePendingStatus
	if err := json.Unmarshal(statusBody2, &status2); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if status2.Failed {
		t.Error("Failed still true after dismiss")
	}
}

func TestDownloadExportJSON(t *testing.T) {
	ts := newTestServer(t)

	createResp, _ := ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]any{"key": "ABC", "title": "Example"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create project status = %d", createResp.StatusCode)
	}

	resp, body := ts.do(http.MethodGet, "/admin/export", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download export status = %d, body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var env backup.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if len(env.Projects) != 1 || env.Projects[0].Key != "ABC" {
		t.Errorf("envelope projects = %+v, want one project ABC", env.Projects)
	}
}

func TestDownloadExportArchiveIncludesAttachments(t *testing.T) {
	ts := newTestServer(t)

	resp, body := ts.do(http.MethodGet, "/admin/export?attachments=true", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download export archive status = %d, body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", ct)
	}

	zipPath := filepath.Join(t.TempDir(), "export.zip")
	if err := os.WriteFile(zipPath, body, 0o644); err != nil {
		t.Fatalf("write zip: %v", err)
	}
	extractDir := filepath.Join(t.TempDir(), "extracted")
	if err := backup.ExtractZip(zipPath, extractDir); err != nil {
		t.Fatalf("ExtractZip: %v", err)
	}
	if _, err := os.Stat(filepath.Join(extractDir, "envelope.json")); err != nil {
		t.Errorf("envelope.json missing from export archive: %v", err)
	}
}

func TestIntegrityReportOnCleanDatabase(t *testing.T) {
	ts := newTestServer(t)

	resp, body := ts.do(http.MethodGet, "/admin/integrity", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("integrity report status = %d, body=%s", resp.StatusCode, body)
	}
	var report backup.IntegrityReport
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if !report.DatabaseOK {
		t.Error("DatabaseOK = false, want true on a fresh database")
	}
	if len(report.OrphanedBlobs) != 0 {
		t.Errorf("OrphanedBlobs = %v, want none", report.OrphanedBlobs)
	}
}

func TestRunGCRequiresConfirm(t *testing.T) {
	ts := newTestServer(t)

	resp, body := ts.do(http.MethodPost, "/admin/integrity/gc", nil, mustJSON(t, map[string]any{"confirm": false}))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("gc without confirm status = %d, body=%s, want 400", resp.StatusCode, body)
	}

	resp2, body2 := ts.do(http.MethodPost, "/admin/integrity/gc", nil, mustJSON(t, map[string]any{"confirm": true}))
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("gc with confirm status = %d, body=%s", resp2.StatusCode, body2)
	}
}

func TestSetupImportRefusesNonEmptyTarget(t *testing.T) {
	ts := newTestServer(t)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("envelope", "envelope.json")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(mustJSON(t, backup.Envelope{FormatVersion: 1, SchemaVersion: 1})); err != nil {
		t.Fatalf("write envelope: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, ts.url+"/api/v1/setup/import", &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("setup import: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup import status = %d, want 200 (report, not error)", resp.StatusCode)
	}
	var report backup.ImportReport
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.Committed {
		t.Error("Committed = true against a non-empty target, want false")
	}
	if len(report.Problems) == 0 {
		t.Error("Problems is empty, want a reason the import was refused")
	}
}

func TestSetupImportCommitsAgainstFreshDatabase(t *testing.T) {
	source := newTestServer(t)
	createResp, _ := source.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]any{"key": "ABC", "title": "Example"}))
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create project status = %d", createResp.StatusCode)
	}
	exportResp, exportBody := source.do(http.MethodGet, "/admin/export", nil, nil)
	if exportResp.StatusCode != http.StatusOK {
		t.Fatalf("download export status = %d", exportResp.StatusCode)
	}
	sourceDataDir := dataDir

	freshDataDir := t.TempDir()
	st, err := store.Open(freshDataDir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	blobs, err := blobstore.Open(freshDataDir)
	if err != nil {
		t.Fatalf("blobstore.Open: %v", err)
	}
	svc := service.New(st, blobs)
	SetDataDir(freshDataDir)
	t.Cleanup(func() { SetDataDir(sourceDataDir) })

	srv := httptest.NewServer(NewHandler(svc, false))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("envelope", "envelope.json")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(exportBody); err != nil {
		t.Fatalf("write envelope: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/setup/import", &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("setup import: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup import status = %d, body=%s", resp.StatusCode, respBody)
	}
	var report backup.ImportReport
	if err := json.Unmarshal(respBody, &report); err != nil {
		t.Fatalf("decode report: %v (body=%s)", err, respBody)
	}
	if !report.Committed {
		t.Fatalf("Committed = false, problems=%v", report.Problems)
	}
}
