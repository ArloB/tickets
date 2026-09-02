package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// parseContentItemRef mirrors parseDecisionRef: a reference that parses
// but names the wrong kind is validation_failed, not a generic
// not_found. kind is the specific kind this route promises (KindPlan
// for every /plans/... route, KindDocument for every /documents/...
// route) — not just "some content item kind" — so GET/PATCH
// /documents/{ref} can't be used to read or write a plan by passing its
// ABC-P1 ref (and vice versa). A single generic parseContentItemRef
// accepting either kind would let a plan ref succeed against a
// /documents/... route purely because both happen to resolve through
// the same content_items table; binding kind per route closes that.
func parseContentItemRef(kind domain.EntityKind, s string) (domain.Reference, *service.Error) {
	ref, err := domain.Parse(s)
	if err != nil {
		return domain.Reference{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: err.Error()}
	}
	if ref.Kind != kind {
		return domain.Reference{}, &service.Error{Code: domain.ErrValidationFailed, Field: "ref", Message: "reference must be a " + string(kind) + " reference"}
	}
	return ref, nil
}

type createContentItemRequest struct {
	Title          string `json:"title"`
	Representation string `json:"representation"`
	Body           string `json:"body"`
	Path           string `json:"path"`
	URL            string `json:"url"`
}

// isMultipartRequest reports whether r's body is multipart/form-data
// — mirrors attachments.go's isMultipart, reused for content items'
// identical create/update dispatch (multipart means a file
// representation; anything else is parsed as JSON).
func isMultipartRequest(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data")
}

// createContentItem returns a handler bound to kind (KindPlan or
// KindDocument) — the create route's URL prefix (/plans vs /documents)
// already says which, so the kind travels as a closure argument rather
// than a request field, matching plan.md Step 3's routing design.
// Dispatches on Content-Type the same way addAttachment does:
// multipart/form-data means representation=file, anything else is
// parsed as JSON naming one of markdown/path/url via its
// "representation" field.
func (s *Server) createContentItem(kind domain.EntityKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectKey := r.PathValue("key")
		if isMultipartRequest(r) {
			s.createFileContentItem(w, r, kind, projectKey)
			return
		}
		s.createNonFileContentItem(w, r, kind, projectKey)
	}
}

func (s *Server) createFileContentItem(w http.ResponseWriter, r *http.Request, kind domain.EntityKind, projectKey string) {
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

	// No idempotency key: a multipart upload's body can't be
	// fingerprinted the way Fingerprint hashes a JSON body (matching
	// addAttachment's own multipart create path, which skips
	// idempotency for the same reason).
	item, err := s.svc.CreateContentItem(r.Context(), service.CreateContentItemRequest{
		ProjectKey: projectKey, Kind: kind, Title: title, Representation: domain.ContentRepresentationFile,
		Content: f, FileName: fileHeaders[0].Filename, MediaType: mediaType,
	}, requestActor(r), correlationID(r), "", "")
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toContentItemDetail(item))
}

func (s *Server) createNonFileContentItem(w http.ResponseWriter, r *http.Request, kind domain.EntityKind, projectKey string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req createContentItemRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	fp, ferr := service.Fingerprint(r.Method, r.URL.Path, body)
	if ferr != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: ferr.Error()})
		return
	}

	representation := domain.ContentRepresentation(req.Representation)
	if representation == "" {
		representation = domain.ContentRepresentationMarkdown
	}
	item, err := s.svc.CreateContentItem(r.Context(), service.CreateContentItemRequest{
		ProjectKey: projectKey, Kind: kind, Title: req.Title, Representation: representation,
		Body: req.Body, PathValue: req.Path, URLValue: req.URL,
	}, requestActor(r), correlationID(r), idempotencyKey(r), fp)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toContentItemDetail(item))
}

func (s *Server) listContentItems(kind domain.EntityKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectKey := r.PathValue("key")
		q := r.URL.Query()

		limit := 0
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Field: "limit", Message: "limit must be a non-negative integer"})
				return
			}
			limit = n
		}
		includeArchived := q.Get("include_archived") == "true"

		result, err := s.svc.ListContentItems(r.Context(), projectKey, kind, limit, q.Get("cursor"), includeArchived)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out := make([]contentItemCompact, len(result.Items))
		for i, item := range result.Items {
			out[i] = toContentItemCompact(item)
		}
		writeJSON(w, http.StatusOK, contentItemsPage{Items: out, NextCursor: result.NextCursor})
	}
}

func (s *Server) getContentItem(kind domain.EntityKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref, svcErr := parseContentItemRef(kind, r.PathValue("ref"))
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}
		item, err := s.svc.GetContentItem(r.Context(), ref)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, toContentItemDetail(item))
	}
}

type updateContentItemRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Path  string `json:"path"`
	URL   string `json:"url"`
}

// updateContentItem returns a handler bound to kind, dispatching on
// Content-Type the same way createContentItem does. Representation
// itself is never accepted here — it's immutable, inferred
// server-side from the existing row (service.UpdateContentItem's own
// doc explains why).
func (s *Server) updateContentItem(kind domain.EntityKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref, svcErr := parseContentItemRef(kind, r.PathValue("ref"))
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}
		version, svcErr := parseIfMatch(r)
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}
		if isMultipartRequest(r) {
			s.updateFileContentItem(w, r, ref, version)
			return
		}
		s.updateNonFileContentItem(w, r, ref, version)
	}
}

func (s *Server) updateFileContentItem(w http.ResponseWriter, r *http.Request, ref domain.Reference, expectedVersion int64) {
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

	item, err := s.svc.UpdateContentItem(r.Context(), service.UpdateContentItemRequest{
		Ref: ref, Title: title, Content: f, FileName: fileHeaders[0].Filename, MediaType: mediaType,
		ExpectedVersion: expectedVersion,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toContentItemDetail(item))
}

func (s *Server) updateNonFileContentItem(w http.ResponseWriter, r *http.Request, ref domain.Reference, expectedVersion int64) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
		return
	}
	var req updateContentItemRequest
	if svcErr := decodeJSON(body, &req); svcErr != nil {
		writeError(w, r, svcErr)
		return
	}

	item, err := s.svc.UpdateContentItem(r.Context(), service.UpdateContentItemRequest{
		Ref: ref, Title: req.Title, Body: req.Body, PathValue: req.Path, URLValue: req.URL,
		ExpectedVersion: expectedVersion,
	}, requestActor(r), correlationID(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toContentItemDetail(item))
}

type updateContentItemStatusRequest struct {
	Status string `json:"status"`
}

// updateContentItemStatus returns a handler bound to kind — POST
// /plans|documents/{ref}/status, archive or unarchive, mirroring
// updateProjectStatus (ADR 0028).
func (s *Server) updateContentItemStatus(kind domain.EntityKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref, svcErr := parseContentItemRef(kind, r.PathValue("ref"))
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}
		version, svcErr := parseIfMatch(r)
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Message: "failed to read request body"})
			return
		}
		var req updateContentItemStatusRequest
		if svcErr := decodeJSON(body, &req); svcErr != nil {
			writeError(w, r, svcErr)
			return
		}

		item, err := s.svc.SetContentItemStatus(r.Context(), service.SetContentItemStatusRequest{
			Ref: ref, NewStatus: domain.ContentItemStatus(req.Status), ExpectedVersion: version,
		}, requestActor(r), correlationID(r))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, toContentItemDetail(item))
	}
}

func (s *Server) listContentItemVersions(kind domain.EntityKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref, svcErr := parseContentItemRef(kind, r.PathValue("ref"))
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}
		versions, err := s.svc.ListContentItemVersions(r.Context(), ref)
		if err != nil {
			writeError(w, r, err)
			return
		}
		out := make([]contentItemVersionEntry, len(versions))
		for i, v := range versions {
			out[i] = toContentItemVersionEntry(v)
		}
		writeJSON(w, http.StatusOK, contentItemVersionsPage{Versions: out})
	}
}

func (s *Server) getContentItemDiff(kind domain.EntityKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref, svcErr := parseContentItemRef(kind, r.PathValue("ref"))
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}
		q := r.URL.Query()
		from, svcErr := parseDiffVersionParam(q.Get("from"), "from")
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}
		to, svcErr := parseDiffVersionParam(q.Get("to"), "to")
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}

		diff, err := s.svc.GetContentItemDiff(r.Context(), ref, from, to)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, toContentItemDiff(diff))
	}
}

// downloadContentItem is GET /plans|documents/{ref}/download — streams
// a file-representation content item's current bytes, mirroring
// downloadAttachment.
func (s *Server) downloadContentItem(kind domain.EntityKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref, svcErr := parseContentItemRef(kind, r.PathValue("ref"))
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}
		dl, err := s.svc.DownloadContentItem(r.Context(), ref)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeAttachmentDownload(w, r, service.AttachmentDownload(dl))
	}
}

func (s *Server) downloadContentItemVersion(kind domain.EntityKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref, svcErr := parseContentItemRef(kind, r.PathValue("ref"))
		if svcErr != nil {
			writeError(w, r, svcErr)
			return
		}
		version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
		if err != nil || version < 1 {
			writeError(w, r, &service.Error{Code: domain.ErrValidationFailed, Field: "version", Message: "version must be a positive integer"})
			return
		}
		dl, svcErr2 := s.svc.DownloadContentItemVersion(r.Context(), ref, version)
		if svcErr2 != nil {
			writeError(w, r, svcErr2)
			return
		}
		writeAttachmentDownload(w, r, service.AttachmentDownload(dl))
	}
}
