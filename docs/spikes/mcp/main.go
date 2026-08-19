// Command mcp-spike is throwaway Phase 0 scaffolding. It verifies the
// claims ADR 0006 needs from github.com/modelcontextprotocol/go-sdk
// v1.2.0 before the transport decision is written down: one tool
// registration function feeding both Streamable HTTP and stdio, a real
// client reaching a tool over each transport, and
// auth.RequireBearerToken rejecting bad tokens while exposing TokenInfo
// to the tool handler.
//
// Run assertions:  go run ./docs/spikes/mcp
// Run as the stdio subprocess (used internally by assertion 4):
//
//	go run ./docs/spikes/mcp stdio-server
//
// Deleted, along with the rest of docs/spikes/, once both spike reports
// are PASS (Phase 0 verification item 8).
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const validToken = "spike-token-abc123"

type echoInput struct {
	Message string `json:"message" jsonschema:"the text to echo back"`
}

type echoOutput struct {
	Message    string `json:"message"`
	AuthUserID string `json:"auth_user_id,omitempty"`
}

// registerTools is the single tool-registration function shared by both
// the Streamable HTTP server and the stdio server below. Nothing else
// defines the "echo" tool; if both transports can reach it, that alone
// proves assertion 1 is more than structural.
func registerTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "echo",
		Description: "Echoes the input message back, plus the authenticated user ID if the transport carried one.",
	}, func(_ context.Context, req *mcp.CallToolRequest, in echoInput) (*mcp.CallToolResult, echoOutput, error) {
		out := echoOutput{Message: in.Message}
		if req.Extra != nil && req.Extra.TokenInfo != nil {
			out.AuthUserID = req.Extra.TokenInfo.UserID
		}
		return nil, out, nil
	})
}

func newServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "mcp-spike", Version: "0.0.0"}, nil)
	registerTools(s)
	return s
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "stdio-server" {
		runStdioServer()
		return
	}
	runAssertions()
}

// runStdioServer is launched as a subprocess by assertion 4. It is the
// same registerTools() call as the HTTP path, just reached over
// mcp.StdioTransport instead of mcp.NewStreamableHTTPHandler.
func runStdioServer() {
	s := newServer()
	if err := s.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "stdio server exited:", err)
		os.Exit(1)
	}
}

type checkResult struct {
	n    int
	name string
	pass bool
	note string
}

var results []checkResult

func check(n int, name string, pass bool, note string) {
	results = append(results, checkResult{n, name, pass, note})
	status := "PASS"
	if !pass {
		status = "FAIL"
	}
	fmt.Printf("[%d] %-70s %s  %s\n", n, name, status, note)
}

func runAssertions() {
	ctx := context.Background()

	// --- shared server + bearer-auth middleware for the HTTP leg ---
	verifier := func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		if token != validToken {
			return nil, fmt.Errorf("%w: unrecognized token", auth.ErrInvalidToken)
		}
		return &auth.TokenInfo{
			UserID:     "spike-user",
			Scopes:     []string{"read", "write"},
			Expiration: time.Now().Add(time.Hour),
		}, nil
	}
	authMW := auth.RequireBearerToken(verifier, &auth.RequireBearerTokenOptions{
		Scopes:              []string{"read"},
		ResourceMetadataURL: "https://example.invalid/.well-known/oauth-protected-resource",
	})

	sharedServer := newServer()
	httpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return sharedServer }, nil)
	ts := httptest.NewServer(authMW(httpHandler))
	defer ts.Close()

	// --- 1. structural: the same registerTools() call is what both the
	// HTTP server above and the stdio server in runStdioServer() use.
	// Assertions 3 and 4 below exercise that operationally by reaching
	// the "echo" tool through each transport.
	check(1, "one registerTools(server) feeds both transports (see assertions 3+4)", true, "no duplicate tool definitions in this file")

	// --- 2. missing/invalid token returns 401 + WWW-Authenticate ---
	pass, note := checkUnauthorized(ts.URL, "")
	check(2, "no Authorization header -> 401 + WWW-Authenticate", pass, note)
	pass, note = checkUnauthorized(ts.URL, "wrong-token")
	check(2, "invalid Authorization token -> 401 + WWW-Authenticate", pass, note)

	// --- 3. real client over Streamable HTTP with a valid token, TokenInfo visible to the tool ---
	httpOK, httpNote := runHTTPClientTest(ctx, ts.URL)
	check(3, "client over Streamable HTTP: list+call tool, TokenInfo propagated", httpOK, httpNote)

	// --- 4. real client over stdio, launching this binary as a subprocess ---
	stdioOK, stdioNote := runStdioClientTest(ctx)
	check(4, "client over stdio (subprocess): list+call tool", stdioOK, stdioNote)

	printSummaryAndExit()
}

// checkUnauthorized performs a raw MCP initialize POST without (or with
// an invalid) bearer token and confirms the middleware rejects it before
// any client library has a chance to hide the response.
func checkUnauthorized(baseURL, badToken string) (bool, string) {
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"spike","version":"0"}}}`
	req, err := http.NewRequest(http.MethodPost, baseURL, stringsReader(body))
	if err != nil {
		return false, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if badToken != "" {
		req.Header.Set("Authorization", "Bearer "+badToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer func() { _ = resp.Body.Close() }()

	wwwAuth := resp.Header.Get("WWW-Authenticate")
	ok := resp.StatusCode == http.StatusUnauthorized && wwwAuth != ""
	return ok, fmt.Sprintf("status=%d WWW-Authenticate=%q", resp.StatusCode, wwwAuth)
}

func runHTTPClientTest(ctx context.Context, baseURL string) (bool, string) {
	authTransport := &bearerRoundTripper{token: validToken, base: http.DefaultTransport}
	client := mcp.NewClient(&mcp.Implementation{Name: "mcp-spike-client", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   baseURL,
		HTTPClient: &http.Client{Transport: authTransport},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return false, "connect failed: " + err.Error()
	}
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		return false, "ListTools failed: " + err.Error()
	}
	if !hasTool(tools.Tools, "echo") {
		return false, "echo tool not advertised over HTTP"
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"message": "hello over http"},
	})
	if err != nil {
		return false, "CallTool failed: " + err.Error()
	}
	out, err := decodeEchoOutput(res)
	if err != nil {
		return false, err.Error()
	}
	if out.Message != "hello over http" {
		return false, fmt.Sprintf("unexpected echo: %+v", out)
	}
	if out.AuthUserID != "spike-user" {
		return false, fmt.Sprintf("TokenInfo did not propagate to tool handler: auth_user_id=%q", out.AuthUserID)
	}
	return true, fmt.Sprintf("echo=%q auth_user_id=%q", out.Message, out.AuthUserID)
}

func runStdioClientTest(ctx context.Context) (bool, string) {
	self, err := os.Executable()
	if err != nil {
		return false, "os.Executable failed: " + err.Error()
	}
	// `go run` builds a temp binary; os.Executable() correctly resolves
	// to that temp binary, so relaunching it with "stdio-server" reruns
	// this same program, hits main()'s stdio-server branch, and reuses
	// the identical registerTools() call as the HTTP leg above.
	cmd := exec.Command(self, "stdio-server")

	client := mcp.NewClient(&mcp.Implementation{Name: "mcp-spike-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return false, "connect failed: " + err.Error()
	}
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		return false, "ListTools failed: " + err.Error()
	}
	if !hasTool(tools.Tools, "echo") {
		return false, "echo tool not advertised over stdio"
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"message": "hello over stdio"},
	})
	if err != nil {
		return false, "CallTool failed: " + err.Error()
	}
	out, err := decodeEchoOutput(res)
	if err != nil {
		return false, err.Error()
	}
	if out.Message != "hello over stdio" {
		return false, fmt.Sprintf("unexpected echo: %+v", out)
	}
	// No HTTP layer over stdio, so no TokenInfo is expected here.
	return true, fmt.Sprintf("echo=%q (no bearer token over stdio, as expected)", out.Message)
}

func printSummaryAndExit() {
	fail := 0
	for _, r := range results {
		if !r.pass {
			fail++
		}
	}
	fmt.Printf("\n%d/%d assertions passed\n", len(results)-fail, len(results))
	if fail > 0 {
		os.Exit(1)
	}
}
