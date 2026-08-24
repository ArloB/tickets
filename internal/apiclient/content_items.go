package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ArloB/tickets/internal/domain"
)

// ContentItem mirrors internal/httpapi/wire.go's contentItemDetail
// field-for-field (product spec §5.9). Kind is "plan" or "document".
// Body/FileName+FileSize+MediaType+Checksum/PathValue/URLValue are
// mutually exclusive, populated according to Representation.
type ContentItem struct {
	Ref            string    `json:"ref"`
	Project        string    `json:"project"`
	Kind           string    `json:"kind"`
	Title          string    `json:"title"`
	Representation string    `json:"representation"`
	Body           string    `json:"body"`
	FileName       string    `json:"file_name,omitempty"`
	FileSize       int64     `json:"file_size,omitempty"`
	MediaType      string    `json:"media_type,omitempty"`
	Checksum       string    `json:"checksum,omitempty"`
	PathValue      string    `json:"path_value,omitempty"`
	URLValue       string    `json:"url_value,omitempty"`
	Version        int64     `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ContentItemCompact mirrors internal/httpapi/wire.go's
// contentItemCompact — list rows never carry Body.
type ContentItemCompact struct {
	Ref       string    `json:"ref"`
	Title     string    `json:"title"`
	Kind      string    `json:"kind"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ContentItemsPage is GET /projects/{key}/plans|documents' response
// envelope.
type ContentItemsPage struct {
	Items      []ContentItemCompact `json:"items"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

// CreateContentItemRequest is POST /projects/{key}/plans|documents'
// JSON request body for markdown/path/url representations — a file
// representation is created via UploadContentItem instead.
// Representation defaults to "markdown" server-side when empty.
type CreateContentItemRequest struct {
	Title          string `json:"title"`
	Representation string `json:"representation,omitempty"`
	Body           string `json:"body,omitempty"`
	Path           string `json:"path,omitempty"`
	URL            string `json:"url,omitempty"`
}

// UpdateContentItemRequest is PATCH /plans|documents/{ref}'s JSON
// request body — a full-representation update, matching
// UpdateDecisionRequest's contract. No Representation field:
// representation is immutable, inferred server-side from the existing
// item. A file representation is replaced via ReplaceContentItemFile
// instead.
type UpdateContentItemRequest struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Path  string `json:"path,omitempty"`
	URL   string `json:"url,omitempty"`
}

// ContentItemVersion mirrors internal/httpapi/wire.go's
// contentItemVersionEntry.
type ContentItemVersion struct {
	Version        int64     `json:"version"`
	Representation string    `json:"representation"`
	Title          string    `json:"title"`
	Body           string    `json:"body"`
	FileName       string    `json:"file_name,omitempty"`
	FileSize       int64     `json:"file_size,omitempty"`
	MediaType      string    `json:"media_type,omitempty"`
	Checksum       string    `json:"checksum,omitempty"`
	PathValue      string    `json:"path_value,omitempty"`
	URLValue       string    `json:"url_value,omitempty"`
	EditedBy       string    `json:"edited_by"`
	CreatedAt      time.Time `json:"created_at"`
}

// ContentItemVersionsPage is GET /plans|documents/{ref}/versions'
// response envelope.
type ContentItemVersionsPage struct {
	Versions []ContentItemVersion `json:"versions"`
}

// ContentItemDiff is GET /plans|documents/{ref}/diff's response shape.
type ContentItemDiff struct {
	FromVersion int64      `json:"from_version"`
	ToVersion   int64      `json:"to_version"`
	Title       []DiffLine `json:"title"`
	Body        []DiffLine `json:"body"`
}

// Every method below takes urlKind ("plans" or "documents") as its
// first argument rather than existing as a CreatePlan/CreateDocument
// pair — a code-review pass on an earlier version of this file (twelve
// near-identical methods differing only in URL segment) flagged that
// as real duplication risk, independently, from three separate review
// angles. cmd/tickets/content_item.go and web/src/api/content-items.ts
// already parameterize the same way (ContentItemUrlKind), so this
// mirrors both rather than being the one layer that didn't generalize.

// CreateContentItem is POST /projects/{key}/{urlKind}.
func (c *Client) CreateContentItem(ctx context.Context, urlKind, projectKey string, req CreateContentItemRequest, idempotencyKey string) (ContentItem, error) {
	var item ContentItem
	err := c.do(ctx, http.MethodPost, "/projects/"+url.PathEscape(projectKey)+"/"+urlKind, req, &item, requestOptions{IdempotencyKey: idempotencyKey})
	return item, err
}

// GetContentItem is GET /{urlKind}/{ref}.
func (c *Client) GetContentItem(ctx context.Context, urlKind, ref string) (ContentItem, error) {
	var item ContentItem
	err := c.do(ctx, http.MethodGet, "/"+urlKind+"/"+url.PathEscape(ref), nil, &item, requestOptions{})
	return item, err
}

// ListContentItems is GET /projects/{key}/{urlKind}. Compact rows only.
func (c *Client) ListContentItems(ctx context.Context, urlKind, projectKey string, limit int, cursor string) (ContentItemsPage, error) {
	var page ContentItemsPage
	path := "/projects/" + url.PathEscape(projectKey) + "/" + urlKind + listQuery(nil, limit, cursor)
	err := c.do(ctx, http.MethodGet, path, nil, &page, requestOptions{})
	return page, err
}

// UpdateContentItem is PATCH /{urlKind}/{ref}.
func (c *Client) UpdateContentItem(ctx context.Context, urlKind, ref string, req UpdateContentItemRequest, expectedVersion int64) (ContentItem, error) {
	var item ContentItem
	err := c.do(ctx, http.MethodPatch, "/"+urlKind+"/"+url.PathEscape(ref), req, &item, requestOptions{IfMatch: &expectedVersion})
	return item, err
}

// ListContentItemVersions is GET /{urlKind}/{ref}/versions.
func (c *Client) ListContentItemVersions(ctx context.Context, urlKind, ref string) (ContentItemVersionsPage, error) {
	var page ContentItemVersionsPage
	err := c.do(ctx, http.MethodGet, "/"+urlKind+"/"+url.PathEscape(ref)+"/versions", nil, &page, requestOptions{})
	return page, err
}

// GetContentItemDiff is GET /{urlKind}/{ref}/diff?from=&to=.
func (c *Client) GetContentItemDiff(ctx context.Context, urlKind, ref string, from, to int64) (ContentItemDiff, error) {
	var diff ContentItemDiff
	path := "/" + urlKind + "/" + url.PathEscape(ref) + "/diff?from=" + strconv.FormatInt(from, 10) + "&to=" + strconv.FormatInt(to, 10)
	err := c.do(ctx, http.MethodGet, path, nil, &diff, requestOptions{})
	return diff, err
}

// UploadContentItem creates a file-representation plan or document —
// POST /projects/{key}/{urlKind}, multipart/form-data. Mirrors
// UploadAttachment: content is streamed straight into the request
// body, never buffered whole.
func (c *Client) UploadContentItem(ctx context.Context, urlKind, projectKey, title, fileName, mediaType string, content io.Reader) (ContentItem, error) {
	path := "/projects/" + url.PathEscape(projectKey) + "/" + urlKind
	return c.doMultipartContentItem(ctx, http.MethodPost, path, nil, title, fileName, mediaType, content)
}

// ReplaceContentItemFile stores a new version as a file-representation
// item's current state — PATCH /{urlKind}/{ref}, multipart/form-data.
func (c *Client) ReplaceContentItemFile(ctx context.Context, urlKind, ref, title, fileName, mediaType string, content io.Reader, expectedVersion int64) (ContentItem, error) {
	path := "/" + urlKind + "/" + url.PathEscape(ref)
	return c.doMultipartContentItem(ctx, http.MethodPatch, path, &expectedVersion, title, fileName, mediaType, content)
}

func (c *Client) doMultipartContentItem(ctx context.Context, method, path string, ifMatch *int64, title, fileName, mediaType string, content io.Reader) (ContentItem, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		defer func() { _ = pw.Close() }()
		if err := mw.WriteField("title", title); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if mediaType != "" {
			if err := mw.WriteField("media_type", mediaType); err != nil {
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
		return ContentItem{}, fmt.Errorf("apiclient: build request: %w", err)
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
		return ContentItem{}, fmt.Errorf("apiclient: request %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		var env errorEnvelope
		if decErr := json.NewDecoder(resp.Body).Decode(&env); decErr != nil || env.Error.Code == "" {
			return ContentItem{}, fmt.Errorf("apiclient: %s %s returned status %d with no decodable error body", method, path, resp.StatusCode)
		}
		return ContentItem{}, &Error{Code: domain.ErrorCode(env.Error.Code), Message: env.Error.Message, Field: env.Error.Field, CurrentVersion: env.Error.CurrentVersion}
	}

	var out ContentItem
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ContentItem{}, fmt.Errorf("apiclient: decode response from %s %s: %w", method, path, err)
	}
	return out, nil
}

// ContentItemDownload is DownloadContentItem/DownloadContentItemVersion's
// result — callers must Close Content.
type ContentItemDownload struct {
	Content     io.ReadCloser
	FileName    string
	ContentType string
}

// DownloadContentItem streams a file-representation plan or document's
// current bytes. The caller must Close the returned Content.
func (c *Client) DownloadContentItem(ctx context.Context, urlKind, ref string) (ContentItemDownload, error) {
	return c.downloadContentItem(ctx, "/"+urlKind+"/"+url.PathEscape(ref)+"/download")
}

// DownloadContentItemVersion streams one archived version's bytes.
func (c *Client) DownloadContentItemVersion(ctx context.Context, urlKind, ref string, version int64) (ContentItemDownload, error) {
	return c.downloadContentItem(ctx, "/"+urlKind+"/"+url.PathEscape(ref)+"/versions/"+strconv.FormatInt(version, 10)+"/download")
}

func (c *Client) downloadContentItem(ctx context.Context, path string) (ContentItemDownload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return ContentItemDownload{}, fmt.Errorf("apiclient: build request: %w", err)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("X-Correlation-Id", newCorrelationID())

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return ContentItemDownload{}, fmt.Errorf("apiclient: request GET %s: %w", path, err)
	}
	if resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		var env errorEnvelope
		if decErr := json.NewDecoder(resp.Body).Decode(&env); decErr != nil || env.Error.Code == "" {
			return ContentItemDownload{}, fmt.Errorf("apiclient: GET %s returned status %d with no decodable error body", path, resp.StatusCode)
		}
		return ContentItemDownload{}, &Error{Code: domain.ErrorCode(env.Error.Code), Message: env.Error.Message, Field: env.Error.Field, CurrentVersion: env.Error.CurrentVersion}
	}

	fileName := parseContentDispositionFileName(resp.Header.Get("Content-Disposition"))
	return ContentItemDownload{Content: resp.Body, FileName: fileName, ContentType: resp.Header.Get("Content-Type")}, nil
}
