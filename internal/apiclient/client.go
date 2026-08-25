// Package apiclient is a minimal HTTP client for the Tickets
// /api/v1 surface — the plan's Step 15 lift of internal/mcpsrv's
// HTTPBackend HTTP-plumbing and error-envelope decoding into a
// standalone package, so `tickets mcp`'s stdio bridge (and any future
// Phase 3 CLI command) has one shared client rather than duplicated
// net/http/json.Decoder boilerplate. Scoped to exactly what today's
// callers need — GetProject, GetTicket, CreateTicket — not a
// generated full-surface client: hand-written is the right amount of
// investment for what this phase's MCP tool surface actually calls;
// full codegen tooling is better left until a later phase needs the
// complete route surface.
package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/google/uuid"
)

// Client talks to a running Tickets server's /api/v1 HTTP API.
type Client struct {
	BaseURL    string // e.g. "http://127.0.0.1:8080/api/v1"
	HTTPClient *http.Client
	// Token is the agent bearer token attached as
	// "Authorization: Bearer <token>" on every request (ADR 0004).
	// Client does not itself verify this — it just forwards whatever
	// its caller configured it with; the server on the other end
	// verifies it the same way it verifies any other HTTP client's
	// bearer token (ADR 0005).
	Token string
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// errorEnvelope mirrors internal/httpapi's wire shape (docs/contracts/
// errors.md) so an HTTP error response decodes into an *Error a caller
// can inspect by domain.ErrorCode, not just by message string.
type errorEnvelope struct {
	Error struct {
		Code           string `json:"code"`
		Message        string `json:"message"`
		Field          string `json:"field"`
		CurrentVersion *int64 `json:"current_version"`
	} `json:"error"`
}

// requestOptions carries the three per-request concerns docs/contracts/
// concurrency.md and errors.md define (If-Match, Idempotency-Key,
// X-Correlation-Id) so do() handles all three in one place instead of
// each method bolting on its own header logic. Read/get methods pass
// the zero value: no If-Match (nothing to match), no Idempotency-Key
// (reads are always safe to retry per §8.4), and do() still generates
// a correlation ID for them since every request benefits from one.
type requestOptions struct {
	// IfMatch is the entity version an update must match
	// (docs/contracts/concurrency.md); nil means "don't send If-Match
	// at all", which is only correct for a non-mutating request — every
	// mutating method in this package must set this.
	IfMatch *int64
	// IdempotencyKey, if non-empty, is sent so a retried create is safe
	// to repeat (docs/contracts/concurrency.md's fingerprint mechanism).
	IdempotencyKey string
	// CorrelationID overrides the correlation ID do() would otherwise
	// generate itself. Almost every caller leaves this empty.
	CorrelationID string
}

func newCorrelationID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return "unknown"
	}
	return id.String()
}

func (c *Client) do(ctx context.Context, method, path string, reqBody, out any, opts requestOptions) error {
	var bodyReader *bytes.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("apiclient: marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("apiclient: build request: %w", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if opts.IfMatch != nil {
		// internal/httpapi/concurrency.go's parseIfMatch requires a
		// quoted decimal integer, e.g. "3" (the literal quote
		// characters, not Go string-literal quoting).
		req.Header.Set("If-Match", `"`+strconv.FormatInt(*opts.IfMatch, 10)+`"`)
	}
	if opts.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", opts.IdempotencyKey)
	}
	correlationID := opts.CorrelationID
	if correlationID == "" {
		correlationID = newCorrelationID()
	}
	req.Header.Set("X-Correlation-Id", correlationID)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("apiclient: request %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		var env errorEnvelope
		if decErr := json.NewDecoder(resp.Body).Decode(&env); decErr != nil || env.Error.Code == "" {
			return fmt.Errorf("apiclient: %s %s returned status %d with no decodable error body", method, path, resp.StatusCode)
		}
		return &Error{
			Code:           domain.ErrorCode(env.Error.Code),
			Message:        env.Error.Message,
			Field:          env.Error.Field,
			CurrentVersion: env.Error.CurrentVersion,
		}
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("apiclient: decode response from %s %s: %w", method, path, err)
		}
	}
	return nil
}

// GetProject is GET /projects/{key}.
func (c *Client) GetProject(ctx context.Context, key string) (Project, error) {
	var proj Project
	err := c.do(ctx, http.MethodGet, "/projects/"+url.PathEscape(key), nil, &proj, requestOptions{})
	return proj, err
}

// CreateProjectRequest is POST /projects' request body.
type CreateProjectRequest struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// CreateProject is POST /projects.
func (c *Client) CreateProject(ctx context.Context, req CreateProjectRequest, idempotencyKey string) (Project, error) {
	var proj Project
	err := c.do(ctx, http.MethodPost, "/projects", req, &proj, requestOptions{IdempotencyKey: idempotencyKey})
	return proj, err
}

// GetTicket is GET /tickets/{ref}.
func (c *Client) GetTicket(ctx context.Context, ref string) (Ticket, error) {
	var ticket Ticket
	err := c.do(ctx, http.MethodGet, "/tickets/"+url.PathEscape(ref), nil, &ticket, requestOptions{})
	return ticket, err
}

// GetTicketFields is GET /tickets/{ref} with ?fields=/?include=
// (docs/contracts/representations.md), for a caller that wants the
// server's own projected/expanded JSON rather than GetTicket's always-
// complete shape. It deliberately decodes into map[string]any instead
// of Ticket: a ?fields= response only carries the requested keys, and
// decoding that into Ticket would silently zero-pad every excluded
// field (e.g. status becoming "" rather than absent) — indistinguishable
// from a real empty value, which is worse than not projecting at all.
// ?include=comments/relationships has the same problem in reverse —
// Ticket has no field to hold either sub-resource — so this is also
// the only path that can return them. Pass a nil/empty fields or
// include to omit that query param entirely.
func (c *Client) GetTicketFields(ctx context.Context, ref string, fields, include []string) (map[string]any, error) {
	q := url.Values{}
	if len(fields) > 0 {
		q.Set("fields", strings.Join(fields, ","))
	}
	if len(include) > 0 {
		q.Set("include", strings.Join(include, ","))
	}
	path := "/tickets/" + url.PathEscape(ref)
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var out map[string]any
	err := c.do(ctx, http.MethodGet, path, nil, &out, requestOptions{})
	return out, err
}

// CreateTicket is POST /projects/{key}/tickets.
func (c *Client) CreateTicket(ctx context.Context, projectKey string, req CreateTicketRequest) (Ticket, error) {
	var ticket Ticket
	err := c.do(ctx, http.MethodPost, "/projects/"+url.PathEscape(projectKey)+"/tickets", req, &ticket, requestOptions{})
	return ticket, err
}

// listQuery builds a limit/cursor(/view) query string, omitting any
// zero-value part so a bare GET /projects (no params at all) still
// works exactly as it does today.
func listQuery(extra url.Values, limit int, cursor string) string {
	if extra == nil {
		extra = url.Values{}
	}
	if limit > 0 {
		extra.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		extra.Set("cursor", cursor)
	}
	if len(extra) == 0 {
		return ""
	}
	return "?" + extra.Encode()
}

// ListProjects is GET /projects. Compact rows only — see
// ProjectCompact. includeArchived true also returns archived projects
// (ADR 0021); the server default is active-only.
func (c *Client) ListProjects(ctx context.Context, limit int, cursor string, includeArchived bool) (ProjectsPage, error) {
	q := url.Values{}
	if includeArchived {
		q.Set("include_archived", "true")
	}
	var page ProjectsPage
	err := c.do(ctx, http.MethodGet, "/projects"+listQuery(q, limit, cursor), nil, &page, requestOptions{})
	return page, err
}

// UpdateProject is PATCH /projects/{key} — title/description only;
// see SetProjectStatus for archive/unarchive.
func (c *Client) UpdateProject(ctx context.Context, key, title, description string, expectedVersion int64) (Project, error) {
	var proj Project
	err := c.do(ctx, http.MethodPatch, "/projects/"+url.PathEscape(key),
		struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		}{Title: title, Description: description},
		&proj, requestOptions{IfMatch: &expectedVersion})
	return proj, err
}

// SetProjectStatus is POST /projects/{key}/status — archive or
// unarchive.
func (c *Client) SetProjectStatus(ctx context.Context, key, status string, expectedVersion int64) (Project, error) {
	var proj Project
	err := c.do(ctx, http.MethodPost, "/projects/"+url.PathEscape(key)+"/status",
		struct {
			Status string `json:"status"`
		}{Status: status},
		&proj, requestOptions{IfMatch: &expectedVersion})
	return proj, err
}

// ListTickets is GET /projects/{key}/tickets. view selects
// "priority_queue" (server default when empty) or "issue_register"
// (product spec §7.2's tickets_list). Compact rows only.
func (c *Client) ListTickets(ctx context.Context, projectKey, view string, limit int, cursor string) (TicketsPage, error) {
	q := url.Values{}
	if view != "" {
		q.Set("view", view)
	}
	var page TicketsPage
	path := "/projects/" + url.PathEscape(projectKey) + "/tickets" + listQuery(q, limit, cursor)
	err := c.do(ctx, http.MethodGet, path, nil, &page, requestOptions{})
	return page, err
}

// TicketsPageRaw mirrors TicketsPage's wire shape, but each ticket is
// left as a raw JSON object instead of decoded into TicketCompact —
// ListTicketsFields' return type, for the same reason
// GetTicketFields returns map[string]any instead of Ticket.
type TicketsPageRaw struct {
	Tickets    []map[string]any `json:"tickets"`
	NextCursor string           `json:"next_cursor"`
}

// ListTicketsFields is ListTickets with ?fields=, returning each row's
// server-projected JSON rather than a zero-padded TicketCompact — see
// GetTicketFields' doc comment. Pass a nil/empty fields to omit the
// query param, though a caller with nothing to project should just use
// ListTickets instead.
func (c *Client) ListTicketsFields(ctx context.Context, projectKey, view string, limit int, cursor string, fields []string) (TicketsPageRaw, error) {
	q := url.Values{}
	if view != "" {
		q.Set("view", view)
	}
	if len(fields) > 0 {
		q.Set("fields", strings.Join(fields, ","))
	}
	var page TicketsPageRaw
	path := "/projects/" + url.PathEscape(projectKey) + "/tickets" + listQuery(q, limit, cursor)
	err := c.do(ctx, http.MethodGet, path, nil, &page, requestOptions{})
	return page, err
}

// updateTicketStatus is PATCH /tickets/{ref} (docs/contracts/errors.md's
// narrowly-scoped status transition endpoint, distinct from PUT's
// full-representation update) — unexported: UpdateTicket is the public
// entry point every caller (CLI, MCP) uses; this and
// updateTicketFields are its two building blocks.
func (c *Client) updateTicketStatus(ctx context.Context, ref, status string, expectedVersion int64) (Ticket, error) {
	var ticket Ticket
	err := c.do(ctx, http.MethodPatch, "/tickets/"+url.PathEscape(ref),
		struct {
			Status string `json:"status"`
		}{Status: status},
		&ticket, requestOptions{IfMatch: &expectedVersion})
	return ticket, err
}

// updateTicketFieldsRequest is PUT /tickets/{ref}'s request body — a
// full representation, matching internal/httpapi's updateTicketFieldsRequest
// field-for-field (no pointer fields but Severity: the server always
// overwrites all four, which is exactly why UpdateTicket merges in
// unset fields from a current read before calling this).
type updateTicketFieldsRequest struct {
	Type        string  `json:"type"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Priority    string  `json:"priority"`
	Severity    *string `json:"severity"`
}

func (c *Client) updateTicketFields(ctx context.Context, ref string, req updateTicketFieldsRequest, expectedVersion int64) (Ticket, error) {
	var ticket Ticket
	err := c.do(ctx, http.MethodPut, "/tickets/"+url.PathEscape(ref), req, &ticket, requestOptions{IfMatch: &expectedVersion})
	return ticket, err
}

// UpdateTicketOptions is UpdateTicket's input: a nil field means
// "leave unchanged." ExpectedVersion, if non-nil, is used as every
// mutating call's If-Match — including a PUT this method issues after
// its own merge-fetch GET, so a caller-supplied ExpectedVersion is
// never silently replaced by a version this method happened to read
// along the way (product spec §8.4: a stale If-Match must surface as
// a conflict, not be quietly upgraded out from under the caller). If
// nil, UpdateTicket resolves the version itself from a fresh
// GetTicket first.
type UpdateTicketOptions struct {
	Status                                       *string
	Type, Title, Description, Priority, Severity *string
	ExpectedVersion                              *int64
}

// UpdateTicket applies a status transition (PATCH), a full-field
// update (PUT), or both, in that order, to ref — the single merge
// method both cmd/tickets' `ticket update` and mcpsrv's ticket_update
// tool call, so the "which fields need a current read to merge
// against" logic exists exactly once. If Status is set, PATCH runs
// first; if any of Type/Title/Description/Priority/Severity is set,
// PUT runs second. PUT needs the ticket's full current
// representation to avoid clobbering fields the caller didn't
// mention — this method reads that (from the PATCH's own response
// when one just ran, otherwise from a dedicated GetTicket call) rather
// than requiring the caller to supply every field just to change one.
func (c *Client) UpdateTicket(ctx context.Context, ref string, opts UpdateTicketOptions) (Ticket, error) {
	var ifMatch int64
	var result Ticket
	resultKnown := false

	if opts.ExpectedVersion != nil {
		ifMatch = *opts.ExpectedVersion
	} else {
		// This GET doubles as the merge baseline below, so a fields-only
		// update with no ExpectedVersion needs exactly one request, not
		// two: one GET to learn both the version and the current field
		// values, then (if any field is set) one PUT.
		t, err := c.GetTicket(ctx, ref)
		if err != nil {
			return Ticket{}, err
		}
		result, resultKnown = t, true
		ifMatch = t.Version
	}

	if opts.Status != nil {
		t, err := c.updateTicketStatus(ctx, ref, *opts.Status, ifMatch)
		if err != nil {
			return Ticket{}, err
		}
		result, resultKnown = t, true
		ifMatch = t.Version // PATCH bumped the version; a following PUT must match the new one
	}

	fieldsRequested := opts.Type != nil || opts.Title != nil || opts.Description != nil || opts.Priority != nil || opts.Severity != nil
	if fieldsRequested {
		base := result
		if !resultKnown {
			t, err := c.GetTicket(ctx, ref)
			if err != nil {
				return Ticket{}, err
			}
			base = t
			// Only adopt this read's version as the If-Match target when
			// the caller left ExpectedVersion nil (i.e. this GET *is* the
			// version discovery). When the caller supplied their own
			// ExpectedVersion, keep it — see the type's doc comment.
			if opts.ExpectedVersion == nil {
				ifMatch = t.Version
			}
		}
		req := updateTicketFieldsRequest{
			Type: base.Type, Title: base.Title, Description: base.Description,
			Priority: base.Priority, Severity: base.Severity,
		}
		if opts.Type != nil {
			req.Type = *opts.Type
		}
		if opts.Title != nil {
			req.Title = *opts.Title
		}
		if opts.Description != nil {
			req.Description = *opts.Description
		}
		if opts.Priority != nil {
			req.Priority = *opts.Priority
		}
		if opts.Severity != nil {
			req.Severity = opts.Severity
		}
		t, err := c.updateTicketFields(ctx, ref, req, ifMatch)
		if err != nil {
			return Ticket{}, err
		}
		result, resultKnown = t, true
	}

	if !resultKnown {
		// Neither Status nor any field was set — nothing to do but
		// return the current state.
		return c.GetTicket(ctx, ref)
	}
	return result, nil
}

// AssignTicket is POST /tickets/{ref}/assign. assignee is the wire's
// "kind:name" string; nil unassigns.
func (c *Client) AssignTicket(ctx context.Context, ref string, assignee *string, expectedVersion int64) (Ticket, error) {
	var ticket Ticket
	err := c.do(ctx, http.MethodPost, "/tickets/"+url.PathEscape(ref)+"/assign",
		struct {
			Assignee *string `json:"assignee"`
		}{Assignee: assignee},
		&ticket, requestOptions{IfMatch: &expectedVersion})
	return ticket, err
}

// MoveTicket is POST /tickets/{ref}/move.
func (c *Client) MoveTicket(ctx context.Context, ref, featureRef string, expectedVersion int64) (Ticket, error) {
	var ticket Ticket
	err := c.do(ctx, http.MethodPost, "/tickets/"+url.PathEscape(ref)+"/move",
		struct {
			Feature string `json:"feature"`
		}{Feature: featureRef},
		&ticket, requestOptions{IfMatch: &expectedVersion})
	return ticket, err
}

// DeleteTicket is DELETE /tickets/{ref} (soft delete). It returns the
// deleted record's new version, not a full Ticket — matching
// internal/httpapi's deleteResponse, since the server doesn't return
// a full representation of a record it just deleted.
func (c *Client) DeleteTicket(ctx context.Context, ref string, expectedVersion int64) (int64, error) {
	var resp struct {
		Version int64 `json:"version"`
	}
	err := c.do(ctx, http.MethodDelete, "/tickets/"+url.PathEscape(ref), nil, &resp, requestOptions{IfMatch: &expectedVersion})
	return resp.Version, err
}

// RestoreTicket is POST /tickets/{ref}/restore.
func (c *Client) RestoreTicket(ctx context.Context, ref string, expectedVersion int64) (Ticket, error) {
	var ticket Ticket
	err := c.do(ctx, http.MethodPost, "/tickets/"+url.PathEscape(ref)+"/restore", nil, &ticket, requestOptions{IfMatch: &expectedVersion})
	return ticket, err
}
