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

	"github.com/ArloB/tickets/internal/domain"
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

func (c *Client) do(ctx context.Context, method, path string, reqBody, out any) error {
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
	err := c.do(ctx, http.MethodGet, "/projects/"+url.PathEscape(key), nil, &proj)
	return proj, err
}

// GetTicket is GET /tickets/{ref}.
func (c *Client) GetTicket(ctx context.Context, ref string) (Ticket, error) {
	var ticket Ticket
	err := c.do(ctx, http.MethodGet, "/tickets/"+url.PathEscape(ref), nil, &ticket)
	return ticket, err
}

// CreateTicket is POST /projects/{key}/tickets.
func (c *Client) CreateTicket(ctx context.Context, projectKey string, req CreateTicketRequest) (Ticket, error) {
	var ticket Ticket
	err := c.do(ctx, http.MethodPost, "/projects/"+url.PathEscape(projectKey)+"/tickets", req, &ticket)
	return ticket, err
}
