package mcpsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// HTTPBackend implements Backend by calling the configured Tickets
// HTTP API — this is what `tickets mcp` (the stdio bridge) uses, so it
// never opens SQLite directly (product spec §8.1, ADR 0006).
type HTTPBackend struct {
	BaseURL string // e.g. "http://127.0.0.1:8080/api/v1"
	Client  *http.Client
	// Token is the agent bearer token forwarded as Authorization: Bearer
	// on every request (ADR 0004). HTTPBackend does not itself verify
	// this — it just attaches whatever cmd/tickets' `mcp` subcommand was
	// configured with; the server on the other end verifies it the same
	// way it verifies any other HTTP client's bearer token (ADR 0005).
	Token string
}

func (b *HTTPBackend) httpClient() *http.Client {
	if b.Client != nil {
		return b.Client
	}
	return http.DefaultClient
}

// errorEnvelope mirrors internal/httpapi's wire shape (docs/contracts/
// errors.md) so an HTTP error round-trips back into the same
// *service.Error type InProcessBackend produces natively.
type errorEnvelope struct {
	Error struct {
		Code           string `json:"code"`
		Message        string `json:"message"`
		Field          string `json:"field"`
		CurrentVersion *int64 `json:"current_version"`
	} `json:"error"`
}

func (b *HTTPBackend) do(ctx context.Context, method, path string, reqBody any, out any) error {
	var bodyReader *bytes.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("mcpsrv: marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, b.BaseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("mcpsrv: build request: %w", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if b.Token != "" {
		req.Header.Set("Authorization", "Bearer "+b.Token)
	}

	resp, err := b.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("mcpsrv: request %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		var env errorEnvelope
		if decErr := json.NewDecoder(resp.Body).Decode(&env); decErr != nil || env.Error.Code == "" {
			return fmt.Errorf("mcpsrv: %s %s returned status %d with no decodable error body", method, path, resp.StatusCode)
		}
		return &service.Error{
			Code:           domain.ErrorCode(env.Error.Code),
			Message:        env.Error.Message,
			Field:          env.Error.Field,
			CurrentVersion: env.Error.CurrentVersion,
		}
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("mcpsrv: decode response from %s %s: %w", method, path, err)
		}
	}
	return nil
}

func (b *HTTPBackend) GetProject(ctx context.Context, key string) (domain.Project, error) {
	var proj domain.Project
	err := b.do(ctx, http.MethodGet, "/projects/"+url.PathEscape(key), nil, &proj)
	return proj, err
}

func (b *HTTPBackend) GetTicket(ctx context.Context, ref string) (domain.Ticket, error) {
	var ticket domain.Ticket
	err := b.do(ctx, http.MethodGet, "/tickets/"+url.PathEscape(ref), nil, &ticket)
	return ticket, err
}

func (b *HTTPBackend) CreateTicket(ctx context.Context, in CreateTicketInput) (domain.Ticket, error) {
	reqBody := map[string]any{
		"type":        in.Type,
		"title":       in.Title,
		"description": in.Description,
		"priority":    in.Priority,
	}
	if in.Severity != "" {
		reqBody["severity"] = in.Severity
	}
	var ticket domain.Ticket
	err := b.do(ctx, http.MethodPost, "/projects/"+url.PathEscape(in.ProjectKey)+"/tickets", reqBody, &ticket)
	return ticket, err
}
