// This file is the one apiclient file that isn't pure JSON marshal/
// unmarshal — attachment upload and download are streamed multipart/
// binary requests, so its methods build *http.Request directly rather
// than going through do() (product spec §9: never buffer a file whole
// in memory).
package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ArloB/tickets/internal/domain"
)

// Attachment mirrors internal/httpapi/wire.go's attachmentView
// field-for-field (product spec §5.11).
type Attachment struct {
	ID             int64      `json:"id"`
	OwnerRef       string     `json:"owner_ref,omitempty"`
	CommentID      int64      `json:"comment_id,omitempty"`
	Kind           string     `json:"kind"`
	Title          string     `json:"title"`
	CurrentVersion int64      `json:"current_version"`
	FileName       string     `json:"file_name,omitempty"`
	FileSize       int64      `json:"file_size,omitempty"`
	MediaType      string     `json:"media_type,omitempty"`
	Checksum       string     `json:"checksum,omitempty"`
	PathValue      string     `json:"path_value,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	Creator        string     `json:"creator"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
}

// AttachmentsPage is GET .../attachments' response envelope.
type AttachmentsPage struct {
	Attachments []Attachment `json:"attachments"`
}

// AttachmentVersion mirrors internal/httpapi/wire.go's
// attachmentVersionEntry.
type AttachmentVersion struct {
	Version    int64     `json:"version"`
	Kind       string    `json:"kind"`
	FileName   string    `json:"file_name,omitempty"`
	FileSize   int64     `json:"file_size,omitempty"`
	MediaType  string    `json:"media_type,omitempty"`
	Checksum   string    `json:"checksum,omitempty"`
	PathValue  string    `json:"path_value,omitempty"`
	UploadedBy string    `json:"uploaded_by"`
	CreatedAt  time.Time `json:"created_at"`
}

// AttachmentVersionsPage is GET /attachments/{id}/versions' response
// envelope.
type AttachmentVersionsPage struct {
	Versions []AttachmentVersion `json:"versions"`
}

// AttachmentDownload is DownloadAttachment/DownloadAttachmentVersion's
// result — callers must Close Content.
type AttachmentDownload struct {
	Content     io.ReadCloser
	FileName    string
	ContentType string
}

// attachmentOwnerPathPrefix picks /tickets/, /features/, /decisions/,
// /plans/, or /documents/ based on ref's own kind — internal/httpapi
// mounts identical attachment routes under all five (product spec
// §5.11 covers every principal record kind, unlike associations'
// narrower ticket/feature/decision span).
func attachmentOwnerPathPrefix(ref string) (string, error) {
	parsed, err := domain.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("apiclient: parse reference %q: %w", ref, err)
	}
	switch parsed.Kind {
	case domain.KindTicket:
		return "/tickets/", nil
	case domain.KindFeature:
		return "/features/", nil
	case domain.KindDecision:
		return "/decisions/", nil
	case domain.KindPlan:
		return "/plans/", nil
	case domain.KindDocument:
		return "/documents/", nil
	default:
		return "", fmt.Errorf("apiclient: attachments are not supported for a %q reference", parsed.Kind)
	}
}

func (c *Client) attachmentsPath(ownerRef string, commentID int64) (string, error) {
	if commentID != 0 {
		return "/comments/" + strconv.FormatInt(commentID, 10) + "/attachments", nil
	}
	prefix, err := attachmentOwnerPathPrefix(ownerRef)
	if err != nil {
		return "", err
	}
	return prefix + url.PathEscape(ownerRef) + "/attachments", nil
}

// UploadAttachment adds an upload-kind attachment to a principal
// entity (ownerRef non-empty) or a comment (commentID non-zero) —
// exactly one must be set, mirroring service.CreateAttachmentRequest.
// content is streamed straight into the request body, never buffered
// whole.
func (c *Client) UploadAttachment(ctx context.Context, ownerRef string, commentID int64, title, fileName, mediaType string, content io.Reader) (Attachment, error) {
	path, err := c.attachmentsPath(ownerRef, commentID)
	if err != nil {
		return Attachment{}, err
	}
	return c.doMultipartAttachment(ctx, http.MethodPost, path, nil, map[string]string{"title": title, "media_type": mediaType}, fileName, content)
}

// AddPathAttachment adds a path-kind attachment.
func (c *Client) AddPathAttachment(ctx context.Context, ownerRef string, commentID int64, title, path, mediaType string) (Attachment, error) {
	p, err := c.attachmentsPath(ownerRef, commentID)
	if err != nil {
		return Attachment{}, err
	}
	var out Attachment
	err = c.do(ctx, http.MethodPost, p, struct {
		Title     string `json:"title"`
		Path      string `json:"path"`
		MediaType string `json:"media_type"`
	}{Title: title, Path: path, MediaType: mediaType}, &out, requestOptions{})
	return out, err
}

// ListAttachments lists a principal entity's or a comment's
// attachments.
func (c *Client) ListAttachments(ctx context.Context, ownerRef string, commentID int64) (AttachmentsPage, error) {
	path, err := c.attachmentsPath(ownerRef, commentID)
	if err != nil {
		return AttachmentsPage{}, err
	}
	var page AttachmentsPage
	err = c.do(ctx, http.MethodGet, path, nil, &page, requestOptions{})
	return page, err
}

// GetAttachment returns one attachment's metadata.
func (c *Client) GetAttachment(ctx context.Context, id int64) (Attachment, error) {
	var out Attachment
	err := c.do(ctx, http.MethodGet, "/attachments/"+strconv.FormatInt(id, 10), nil, &out, requestOptions{})
	return out, err
}

// ListAttachmentVersions returns an attachment's archived versions.
func (c *Client) ListAttachmentVersions(ctx context.Context, id int64) (AttachmentVersionsPage, error) {
	var page AttachmentVersionsPage
	err := c.do(ctx, http.MethodGet, "/attachments/"+strconv.FormatInt(id, 10)+"/versions", nil, &page, requestOptions{})
	return page, err
}

// DownloadAttachment streams an upload attachment's current bytes.
// The caller must Close the returned Content.
func (c *Client) DownloadAttachment(ctx context.Context, id int64) (AttachmentDownload, error) {
	return c.downloadAttachment(ctx, "/attachments/"+strconv.FormatInt(id, 10)+"/download")
}

// DownloadAttachmentVersion streams one archived version's bytes.
func (c *Client) DownloadAttachmentVersion(ctx context.Context, id, version int64) (AttachmentDownload, error) {
	return c.downloadAttachment(ctx, "/attachments/"+strconv.FormatInt(id, 10)+"/versions/"+strconv.FormatInt(version, 10)+"/download")
}

func (c *Client) downloadAttachment(ctx context.Context, path string) (AttachmentDownload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return AttachmentDownload{}, fmt.Errorf("apiclient: build request: %w", err)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("X-Correlation-Id", newCorrelationID())

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return AttachmentDownload{}, fmt.Errorf("apiclient: request GET %s: %w", path, err)
	}
	if resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		var env errorEnvelope
		if decErr := json.NewDecoder(resp.Body).Decode(&env); decErr != nil || env.Error.Code == "" {
			return AttachmentDownload{}, fmt.Errorf("apiclient: GET %s returned status %d with no decodable error body", path, resp.StatusCode)
		}
		return AttachmentDownload{}, &Error{Code: domain.ErrorCode(env.Error.Code), Message: env.Error.Message, Field: env.Error.Field, CurrentVersion: env.Error.CurrentVersion}
	}

	fileName := parseContentDispositionFileName(resp.Header.Get("Content-Disposition"))
	return AttachmentDownload{Content: resp.Body, FileName: fileName, ContentType: resp.Header.Get("Content-Type")}, nil
}

func parseContentDispositionFileName(header string) string {
	_, params, err := mime.ParseMediaType(header)
	if err != nil {
		return ""
	}
	return params["filename"]
}

// ReplaceUploadAttachment stores a new upload version as an
// attachment's current state.
func (c *Client) ReplaceUploadAttachment(ctx context.Context, id int64, fileName, mediaType string, content io.Reader, expectedVersion int64) (Attachment, error) {
	return c.doMultipartAttachment(ctx, http.MethodPut, "/attachments/"+strconv.FormatInt(id, 10), &expectedVersion, map[string]string{"media_type": mediaType}, fileName, content)
}

// ReplacePathAttachment stores a new path-value version as an
// attachment's current state.
func (c *Client) ReplacePathAttachment(ctx context.Context, id int64, path, mediaType string, expectedVersion int64) (Attachment, error) {
	var out Attachment
	err := c.do(ctx, http.MethodPut, "/attachments/"+strconv.FormatInt(id, 10), struct {
		Path      string `json:"path"`
		MediaType string `json:"media_type"`
	}{Path: path, MediaType: mediaType}, &out, requestOptions{IfMatch: &expectedVersion})
	return out, err
}

// DeleteAttachment soft-deletes an attachment.
func (c *Client) DeleteAttachment(ctx context.Context, id, expectedVersion int64) error {
	return c.do(ctx, http.MethodDelete, "/attachments/"+strconv.FormatInt(id, 10), nil, nil, requestOptions{IfMatch: &expectedVersion})
}

// doMultipartAttachment issues a multipart/form-data request (create
// or replace) with a single "file" part streamed straight from
// content, plus the given text fields. ifMatch is nil for create, set
// for replace.
func (c *Client) doMultipartAttachment(ctx context.Context, method, path string, ifMatch *int64, fields map[string]string, fileName string, content io.Reader) (Attachment, error) {
	// mime/multipart's Writer needs a Part's length known up front for
	// nothing (it chunks the body), so this can genuinely stream —
	// io.Pipe connects the writer goroutine to the request body reader
	// without buffering the whole file.
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		defer func() { _ = pw.Close() }()
		for k, v := range fields {
			if v == "" {
				continue
			}
			if err := mw.WriteField(k, v); err != nil {
				_ = pw.CloseWithError(err)
				return
			}
		}
		fw, err := mw.CreateFormFile("file", fileName)
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(fw, content); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = mw.Close()
	}()

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, pr)
	if err != nil {
		return Attachment{}, fmt.Errorf("apiclient: build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if ifMatch != nil {
		req.Header.Set("If-Match", `"`+strconv.FormatInt(*ifMatch, 10)+`"`)
	}
	req.Header.Set("X-Correlation-Id", newCorrelationID())

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Attachment{}, fmt.Errorf("apiclient: request %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		var env errorEnvelope
		if decErr := json.NewDecoder(resp.Body).Decode(&env); decErr != nil || env.Error.Code == "" {
			return Attachment{}, fmt.Errorf("apiclient: %s %s returned status %d with no decodable error body", method, path, resp.StatusCode)
		}
		return Attachment{}, &Error{Code: domain.ErrorCode(env.Error.Code), Message: env.Error.Message, Field: env.Error.Field, CurrentVersion: env.Error.CurrentVersion}
	}

	var out Attachment
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Attachment{}, fmt.Errorf("apiclient: decode response from %s %s: %w", method, path, err)
	}
	return out, nil
}
