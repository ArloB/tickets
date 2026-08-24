package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// maxUploadSize is ADR 0007's configurable per-version upload cap
// (default 25 MiB), enforced here — before the handler body starts
// writing to storage — via http.MaxBytesReader wrapping the request
// body. SetMaxUploadBytes sets it at server startup, from
// internal/config; the package-level var (mirroring SetLogger's own
// pattern) also lets attachments_test.go shrink it directly.
var maxUploadSize int64 = 25 << 20

// SetMaxUploadBytes overrides the attachment upload size limit.
func SetMaxUploadBytes(n int64) {
	maxUploadSize = n
}

// parseAttachmentID parses the {id} path value attachment routes
// share. Attachments have no formatted reference (§6.3's stable-
// reference requirement is for principal record kinds only — an
// attachment has no public reference of its own), the same reasoning
// parseCommentID gives for comments.
func parseAttachmentID(s string) (int64, *service.Error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, &service.Error{Code: domain.ErrValidationFailed, Field: "id", Message: "attachment id must be an integer"}
	}
	return id, nil
}

// addAttachment is POST .../attachments on a ticket, feature,
// decision, plan, or document — one handler reused across all five
// route registrations, the same pattern addLink/addAssociation use
// (parseAssociationRef doesn't restrict which kind ref names).
// Dispatches on Content-Type: multipart/form-data means an upload
// attachment, anything else is parsed as a JSON path-attachment
// request — mirroring ADR 0007's plan ("multipart for kind=upload,
// JSON for path").
func (s *Server) addAttachment(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseAssociationRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	s.createAttachment(w, r, ref, 0)
}

// addCommentAttachment is POST /comments/{id}/attachments.
func (s *Server) addCommentAttachment(w http.ResponseWriter, r *http.Request) {
	id, svcErr := parseCommentID(r.PathValue("id"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	s.createAttachment(w, r, domain.Reference{}, id)
}

func (s *Server) createAttachment(w http.ResponseWriter, r *http.Request, ref domain.Reference, commentID int64) {
	if isMultipart(r) {
		s.createUploadAttachment(w, r, ref, commentID)
		return
	}
	s.createPathAttachment(w, r, ref, commentID)
}

func isMultipart(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data")
}

// createUploadAttachment handles a multipart upload. ParseMultipartForm
// spills anything over its memory threshold straight to a temp file on
// disk rather than holding it in RAM (mime/multipart's own streaming
// behavior), and the file part is then re-streamed into the blobstore
// — never buffered whole in memory (§9), even though this means one
// extra disk copy versus reading the wire stream directly, in exchange
// for order-independent form fields (a client can send "title" after
// "file" and this still works, unlike a single-pass multipart.Reader).
func (s *Server) createUploadAttachment(w http.ResponseWriter, r *http.Request, ref domain.Reference, commentID int64) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
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

	title := r.FormValue("title")
	mediaType := r.FormValue("media_type")
	fileHeaders := r.MultipartForm.File["file"]
	if len(fileHeaders) != 1 {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Field: "file", Message: `exactly one file part named "file" is required`})
		return
	}
	f, err := fileHeaders[0].Open()
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read uploaded file"})
		return
	}
	defer func() { _ = f.Close() }()
	if mediaType == "" {
		mediaType = fileHeaders[0].Header.Get("Content-Type")
	}

	attachment, err := s.svc.CreateAttachment(r.Context(), service.CreateAttachmentRequest{
		Ref: ref, CommentID: commentID, Title: title, Kind: domain.AttachmentKindUpload,
		Content: f, FileName: fileHeaders[0].Filename, MediaType: mediaType,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAttachmentView(attachment))
}

type createPathAttachmentRequest struct {
	Title     string `json:"title"`
	Path      string `json:"path"`
	MediaType string `json:"media_type"`
}

func (s *Server) createPathAttachment(w http.ResponseWriter, r *http.Request, ref domain.Reference, commentID int64) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req createPathAttachmentRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	attachment, err := s.svc.CreateAttachment(r.Context(), service.CreateAttachmentRequest{
		Ref: ref, CommentID: commentID, Title: req.Title, Kind: domain.AttachmentKindPath,
		PathValue: req.Path, MediaType: req.MediaType,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAttachmentView(attachment))
}

// listAttachments is GET .../attachments on a ticket, feature,
// decision, plan, or document.
func (s *Server) listAttachments(w http.ResponseWriter, r *http.Request) {
	ref, svcErr := parseAssociationRef(r.PathValue("ref"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	attachments, err := s.svc.ListAttachmentsForRef(r.Context(), ref)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeAttachmentsPage(w, attachments)
}

// listCommentAttachments is GET /comments/{id}/attachments.
func (s *Server) listCommentAttachments(w http.ResponseWriter, r *http.Request) {
	id, svcErr := parseCommentID(r.PathValue("id"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	attachments, err := s.svc.ListAttachmentsForComment(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeAttachmentsPage(w, attachments)
}

func writeAttachmentsPage(w http.ResponseWriter, attachments []domain.Attachment) {
	out := make([]attachmentView, len(attachments))
	for i, a := range attachments {
		out[i] = toAttachmentView(a)
	}
	writeJSON(w, http.StatusOK, attachmentsPage{Attachments: out})
}

// getAttachment is GET /attachments/{id} — metadata only, the same
// split downloadAttachment draws for bytes.
func (s *Server) getAttachment(w http.ResponseWriter, r *http.Request) {
	id, svcErr := parseAttachmentID(r.PathValue("id"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	attachment, err := s.svc.GetAttachment(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAttachmentView(attachment))
}

func writeAttachmentDownload(w http.ResponseWriter, r *http.Request, dl service.AttachmentDownload) {
	defer func() { _ = dl.Content.Close() }()
	mediaType := dl.MediaType
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mediaType)
	if dl.FileName != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", dl.FileName))
	}
	if dl.FileSize > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(dl.FileSize, 10))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, dl.Content)
}

// downloadAttachment is GET /attachments/{id}/download — streamed, per
// §9, never buffered whole before writing to the response.
func (s *Server) downloadAttachment(w http.ResponseWriter, r *http.Request) {
	id, svcErr := parseAttachmentID(r.PathValue("id"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	dl, err := s.svc.DownloadAttachment(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeAttachmentDownload(w, r, dl)
}

func (s *Server) listAttachmentVersions(w http.ResponseWriter, r *http.Request) {
	id, svcErr := parseAttachmentID(r.PathValue("id"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	versions, err := s.svc.ListAttachmentVersions(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]attachmentVersionEntry, len(versions))
	for i, v := range versions {
		out[i] = toAttachmentVersionEntry(v)
	}
	writeJSON(w, http.StatusOK, attachmentVersionsPage{Versions: out})
}

func (s *Server) downloadAttachmentVersion(w http.ResponseWriter, r *http.Request) {
	id, svcErr := parseAttachmentID(r.PathValue("id"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil || version < 1 {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Field: "version", Message: "version must be a positive integer"})
		return
	}
	dl, svcErr2 := s.svc.DownloadAttachmentVersion(r.Context(), id, version)
	if svcErr2 != nil {
		writeError(w, r, svcErr2)
		return
	}
	writeAttachmentDownload(w, r, dl)
}

// replaceAttachment is PUT /attachments/{id} — stores a new version as
// the current state (§5.11: "each edit saves a full snapshot"), not an
// in-place edit. Dispatches on Content-Type the same way addAttachment
// does.
func (s *Server) replaceAttachment(w http.ResponseWriter, r *http.Request) {
	id, svcErr := parseAttachmentID(r.PathValue("id"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	version, svcErr := parseIfMatch(r)
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	if isMultipart(r) {
		s.replaceUploadAttachment(w, r, id, version)
		return
	}
	s.replacePathAttachment(w, r, id, version)
}

func (s *Server) replaceUploadAttachment(w http.ResponseWriter, r *http.Request, id, expectedVersion int64) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
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

	mediaType := r.FormValue("media_type")
	fileHeaders := r.MultipartForm.File["file"]
	if len(fileHeaders) != 1 {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Field: "file", Message: `exactly one file part named "file" is required`})
		return
	}
	f, err := fileHeaders[0].Open()
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read uploaded file"})
		return
	}
	defer func() { _ = f.Close() }()
	if mediaType == "" {
		mediaType = fileHeaders[0].Header.Get("Content-Type")
	}

	attachment, err := s.svc.ReplaceAttachment(r.Context(), service.ReplaceAttachmentRequest{
		ID: id, Kind: domain.AttachmentKindUpload, Content: f, FileName: fileHeaders[0].Filename,
		MediaType: mediaType, ExpectedVersion: expectedVersion,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAttachmentView(attachment))
}

type replacePathAttachmentRequest struct {
	Path      string `json:"path"`
	MediaType string `json:"media_type"`
}

func (s *Server) replacePathAttachment(w http.ResponseWriter, r *http.Request, id, expectedVersion int64) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req replacePathAttachmentRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	attachment, err := s.svc.ReplaceAttachment(r.Context(), service.ReplaceAttachmentRequest{
		ID: id, Kind: domain.AttachmentKindPath, PathValue: req.Path, MediaType: req.MediaType,
		ExpectedVersion: expectedVersion,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAttachmentView(attachment))
}

// deleteAttachment is DELETE /attachments/{id} — soft-delete; the
// tombstone and every archived version stay visible in history
// (§5.11).
func (s *Server) deleteAttachment(w http.ResponseWriter, r *http.Request) {
	id, svcErr := parseAttachmentID(r.PathValue("id"))
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	version, svcErr := parseIfMatch(r)
	if svcErr != nil {
		writeError(w, r, svcErr)
		return
	}
	if err := s.svc.DeleteAttachment(r.Context(), service.DeleteAttachmentRequest{ID: id, ExpectedVersion: version}, requestActor(r), correlationID(r)); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
