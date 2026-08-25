package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ArloB/tickets/internal/config"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
)

// runAdminAgent is `tickets admin agent <subcommand>` and
// runAdminToken is `tickets admin token <subcommand>` — agent/token
// management, deliberately CLI-only and local-store-only (opening
// internal/store directly, same as runSetup/runAdminPurgeIdempotencyKeys),
// never an MCP tool. InProcessBackend calls *service.Service directly,
// bypassing internal/httpapi's requireAdmin wrapper entirely (a
// bearer-token principal is never IsAdmin), so an agent-management MCP
// tool would be unenforced over the HTTP-mounted endpoint and broken
// over stdio. It's also not a client-mode (--url) command like
// project/ticket/feature/decision: apiclient has no session+CSRF
// support yet, so there is no remote-server path for this today —
// only local, direct-to-store administration, the same trust boundary
// `tickets setup` already uses.
func runAdminAgent(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("admin agent: expected a subcommand (create, list, get)")
	}
	switch args[0] {
	case "create":
		return runAdminAgentCreate(args[1:])
	case "list":
		return runAdminAgentList(args[1:])
	case "get":
		return runAdminAgentGet(args[1:])
	default:
		return fmt.Errorf("admin agent: unknown subcommand %q", args[0])
	}
}

func runAdminToken(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("admin token: expected a subcommand (create, list, revoke)")
	}
	switch args[0] {
	case "create":
		return runAdminTokenCreate(args[1:])
	case "list":
		return runAdminTokenList(args[1:])
	case "revoke":
		return runAdminTokenRevoke(args[1:])
	default:
		return fmt.Errorf("admin token: unknown subcommand %q", args[0])
	}
}

// openAdminService opens internal/store directly and wraps it in a
// *service.Service — see runAdminAgent's doc comment for why this
// stays local-store-only rather than going through apiclient.
func openAdminService(dataDir string) (*store.Store, *service.Service, error) {
	var cfgArgs []string
	if dataDir != "" {
		cfgArgs = []string{"--data-dir", dataDir}
	}
	cfg, err := config.Load(cfgArgs)
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return nil, nil, fmt.Errorf("open store at %s: %w", cfg.DataDir, err)
	}
	return st, service.New(st, nil), nil
}

// parseAsActor resolves --as into the domain.ActorRef these commands
// attribute the mutation to. A bare name is treated as a human account
// — the common case, since product spec §4.1 says an authenticated
// human creates and revokes agent identities and their tokens — while
// a "kind:name" value (e.g. "system:system", the seeded actor every
// installation has from migration 0002) is parsed literally, for
// scripts that have no human account to act as.
func parseAsActor(as string) (domain.ActorRef, error) {
	if as == "" {
		return domain.ActorRef{}, fmt.Errorf("--as is required (the human account performing this action, or a kind:name actor ref such as system:system)")
	}
	ref := domain.ActorRef{Kind: domain.ActorHuman, Name: as}
	if strings.Contains(as, ":") {
		parsed, err := domain.ParseActorRef(as)
		if err != nil {
			return domain.ActorRef{}, err
		}
		ref = parsed
	}
	if ref.Kind == domain.ActorAgent {
		return domain.ActorRef{}, fmt.Errorf("--as must be a human or system actor, not an agent (%s) — product spec §4.1: a human creates and revokes agent identities and their tokens", ref)
	}
	return ref, nil
}

// cliAgentDetail/cliAgentTokenCreated/cliAgentTokenSummary mirror
// internal/httpapi/admin.go's agentDetail/agentTokenCreated/
// agentTokenSummary wire shapes (lowercase snake_case JSON), so
// --json output here matches what the same operations return over
// the session-authenticated HTTP admin surface — this package can't
// import httpapi's unexported types, and shouldn't depend on httpapi
// for a local-store command anyway, so the mapping is duplicated
// rather than shared, the same way apiclient's DTOs duplicate
// httpapi's wire types at every other layer in this codebase.
type cliAgentDetail struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Owner       string    `json:"owner,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func toCLIAgentDetail(d service.AgentDetail) cliAgentDetail {
	out := cliAgentDetail{Name: d.Ref.Name, Description: d.Description, CreatedAt: d.CreatedAt}
	if d.Owner != nil {
		out.Owner = d.Owner.String()
	}
	return out
}

type cliAgentTokenCreated struct {
	ID          int64      `json:"id"`
	Token       string     `json:"token"`
	Description string     `json:"description"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type cliAgentTokenSummary struct {
	ID          int64      `json:"id"`
	Description string     `json:"description"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

func toCLIAgentTokenSummary(t service.AgentTokenSummary) cliAgentTokenSummary {
	return cliAgentTokenSummary{
		ID: t.ID, Description: t.Description, CreatedAt: t.CreatedAt,
		ExpiresAt: t.ExpiresAt, RevokedAt: t.RevokedAt,
	}
}

func runAdminAgentCreate(args []string) error {
	fs := flag.NewFlagSet("admin agent create", flag.ContinueOnError)
	name := fs.String("name", "", "the agent's name (required)")
	description := fs.String("description", "", "optional description")
	as := fs.String("as", "", "the human account performing this action, e.g. arlo (required)")
	dataDir := fs.String("data-dir", "", "directory for the SQLite database (defaults to the same resolution `tickets server` uses)")
	jsonOut := fs.Bool("json", false, "print JSON instead of a human-readable line")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("admin agent create: --name is required")
	}
	actor, err := parseAsActor(*as)
	if err != nil {
		return fmt.Errorf("admin agent create: %w", err)
	}

	st, svc, err := openAdminService(*dataDir)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	detail, err := svc.CreateAgent(context.Background(),
		service.CreateAgentRequest{Name: *name, Description: *description}, actor, service.NewCorrelationID())
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(os.Stdout, toCLIAgentDetail(detail))
	}
	_, err = fmt.Fprintf(os.Stdout, "created agent %s\n", detail.Ref)
	return err
}

func runAdminAgentList(args []string) error {
	fs := flag.NewFlagSet("admin agent list", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", "directory for the SQLite database (defaults to the same resolution `tickets server` uses)")
	jsonOut := fs.Bool("json", false, "print JSON instead of a table")
	if err := fs.Parse(args); err != nil {
		return err
	}

	st, svc, err := openAdminService(*dataDir)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	agents, err := svc.ListAgents(context.Background())
	if err != nil {
		return err
	}
	if *jsonOut {
		out := make([]cliAgentDetail, len(agents))
		for i, a := range agents {
			out[i] = toCLIAgentDetail(a)
		}
		return writeJSON(os.Stdout, out)
	}
	rows := make([][]string, len(agents))
	for i, a := range agents {
		owner := ""
		if a.Owner != nil {
			owner = a.Owner.String()
		}
		rows[i] = []string{a.Ref.Name, owner, a.Description}
	}
	return writeTable(os.Stdout, []string{"NAME", "OWNER", "DESCRIPTION"}, rows)
}

func runAdminAgentGet(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("admin agent get: expected an agent name as the first argument")
	}
	name := args[0]
	fs := flag.NewFlagSet("admin agent get", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", "directory for the SQLite database (defaults to the same resolution `tickets server` uses)")
	jsonOut := fs.Bool("json", false, "print JSON instead of a table")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	st, svc, err := openAdminService(*dataDir)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	detail, err := svc.GetAgentDetail(context.Background(), name)
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(os.Stdout, toCLIAgentDetail(detail))
	}
	owner := ""
	if detail.Owner != nil {
		owner = detail.Owner.String()
	}
	return writeTable(os.Stdout, []string{"NAME", "OWNER", "DESCRIPTION"}, [][]string{{detail.Ref.Name, owner, detail.Description}})
}

func runAdminTokenCreate(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("admin token create: expected an agent name as the first argument")
	}
	agentName := args[0]
	fs := flag.NewFlagSet("admin token create", flag.ContinueOnError)
	description := fs.String("description", "", "optional description")
	expiresIn := fs.Duration("expires-in", 0, "optional expiry, e.g. 720h (default: no expiry)")
	as := fs.String("as", "", "the human account performing this action, e.g. arlo (required)")
	dataDir := fs.String("data-dir", "", "directory for the SQLite database (defaults to the same resolution `tickets server` uses)")
	jsonOut := fs.Bool("json", false, "print JSON instead of a human-readable line")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	actor, err := parseAsActor(*as)
	if err != nil {
		return fmt.Errorf("admin token create: %w", err)
	}

	st, svc, err := openAdminService(*dataDir)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	var expiresAt *time.Time
	if *expiresIn > 0 {
		t := time.Now().UTC().Add(*expiresIn)
		expiresAt = &t
	}

	raw, tokenID, err := svc.CreateAgentToken(context.Background(),
		domain.ActorRef{Kind: domain.ActorAgent, Name: agentName}, *description, expiresAt, actor, service.NewCorrelationID())
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(os.Stdout, cliAgentTokenCreated{ID: tokenID, Token: raw, Description: *description, ExpiresAt: expiresAt})
	}
	_, err = fmt.Fprintf(os.Stdout, "token %d created for agent %s: %s\n(shown once — store it now; it is not logged or retrievable again)\n", tokenID, agentName, raw)
	return err
}

func runAdminTokenList(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("admin token list: expected an agent name as the first argument")
	}
	agentName := args[0]
	fs := flag.NewFlagSet("admin token list", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", "directory for the SQLite database (defaults to the same resolution `tickets server` uses)")
	jsonOut := fs.Bool("json", false, "print JSON instead of a table")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	st, svc, err := openAdminService(*dataDir)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	tokens, err := svc.ListAgentTokens(context.Background(), domain.ActorRef{Kind: domain.ActorAgent, Name: agentName})
	if err != nil {
		return err
	}
	if *jsonOut {
		out := make([]cliAgentTokenSummary, len(tokens))
		for i, t := range tokens {
			out[i] = toCLIAgentTokenSummary(t)
		}
		return writeJSON(os.Stdout, out)
	}
	rows := make([][]string, len(tokens))
	for i, t := range tokens {
		status := "active"
		if t.RevokedAt != nil {
			status = "revoked"
		} else if t.ExpiresAt != nil && t.ExpiresAt.Before(time.Now().UTC()) {
			status = "expired"
		}
		rows[i] = []string{strconv.FormatInt(t.ID, 10), t.Description, status, t.CreatedAt.Format(time.RFC3339)}
	}
	return writeTable(os.Stdout, []string{"ID", "DESCRIPTION", "STATUS", "CREATED_AT"}, rows)
}

func runAdminTokenRevoke(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("admin token revoke: expected a token id as the first argument")
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("admin token revoke: token id must be an integer: %w", err)
	}
	fs := flag.NewFlagSet("admin token revoke", flag.ContinueOnError)
	as := fs.String("as", "", "the human account performing this action, e.g. arlo (required)")
	dataDir := fs.String("data-dir", "", "directory for the SQLite database (defaults to the same resolution `tickets server` uses)")
	jsonOut := fs.Bool("json", false, "print JSON instead of a human-readable line")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	actor, err := parseAsActor(*as)
	if err != nil {
		return fmt.Errorf("admin token revoke: %w", err)
	}

	st, svc, err := openAdminService(*dataDir)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	// service.RevokeAgentToken resolves the token first (Phase 6 Step
	// 1: it needs the owning agent's id to emit an audit event), so it
	// now reports not_found itself instead of store.RevokeAgentToken's
	// idempotent-UPDATE ambiguity — no separate existence check needed
	// here.
	if err := svc.RevokeAgentToken(context.Background(), id, actor, service.NewCorrelationID()); err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(os.Stdout, map[string]any{"id": id, "status": "revoked"})
	}
	_, err = fmt.Fprintf(os.Stdout, "token %d revoked\n", id)
	return err
}
