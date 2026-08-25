package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/ArloB/tickets/internal/service"
)

// runAdminAccount is `tickets admin account <subcommand>` (Phase 7 —
// human account management, product spec §4.2/§13). Local-store-only,
// following runAdminAgent/runAdminToken's own doc comment: internal/
// apiclient has no session/CSRF support, so there is no remote-server
// (--url) path for admin operations today, only direct-to-store
// administration by whoever has CLI/filesystem access to the data
// directory — the same trust boundary `tickets setup` already uses.
func runAdminAccount(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("admin account: expected a subcommand (create, list, change-password)")
	}
	switch args[0] {
	case "create":
		return runAdminAccountCreate(args[1:])
	case "list":
		return runAdminAccountList(args[1:])
	case "change-password":
		return runAdminAccountChangePassword(args[1:])
	default:
		return fmt.Errorf("admin account: unknown subcommand %q", args[0])
	}
}

type cliAccountDetail struct {
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
}

func runAdminAccountCreate(args []string) error {
	fs := flag.NewFlagSet("admin account create", flag.ContinueOnError)
	username := fs.String("username", "", "the new account's username (required)")
	password := fs.String("password", "", "the new account's initial password (required)")
	admin := fs.Bool("admin", false, "grant the operational admin flag (product spec §4.2)")
	as := fs.String("as", "", "the human account performing this action, e.g. arlo (required)")
	dataDir := fs.String("data-dir", "", "directory for the SQLite database (defaults to the same resolution `tickets server` uses)")
	jsonOut := fs.Bool("json", false, "print JSON instead of a human-readable line")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *username == "" {
		return fmt.Errorf("admin account create: --username is required")
	}
	if *password == "" {
		return fmt.Errorf("admin account create: --password is required")
	}
	actor, err := parseAsActor(*as)
	if err != nil {
		return fmt.Errorf("admin account create: %w", err)
	}

	st, svc, err := openAdminService(*dataDir)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	ref, err := svc.CreateHumanAccount(context.Background(),
		service.CreateHumanAccountRequest{Username: *username, Password: *password, IsAdmin: *admin},
		actor, service.NewCorrelationID())
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(os.Stdout, cliAccountDetail{Username: ref.Name, IsAdmin: *admin})
	}
	_, err = fmt.Fprintf(os.Stdout, "created account %s\n", ref.Name)
	return err
}

func runAdminAccountList(args []string) error {
	fs := flag.NewFlagSet("admin account list", flag.ContinueOnError)
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

	accounts, err := svc.ListHumanAccounts(context.Background())
	if err != nil {
		return err
	}
	if *jsonOut {
		out := make([]cliAccountDetail, len(accounts))
		for i, a := range accounts {
			out[i] = cliAccountDetail{Username: a.Username, IsAdmin: a.IsAdmin}
		}
		return writeJSON(os.Stdout, out)
	}
	rows := make([][]string, len(accounts))
	for i, a := range accounts {
		admin := ""
		if a.IsAdmin {
			admin = "yes"
		}
		rows[i] = []string{a.Username, admin, a.CreatedAt.Format("2006-01-02")}
	}
	return writeTable(os.Stdout, []string{"USERNAME", "ADMIN", "CREATED"}, rows)
}

// runAdminAccountChangePassword resets a password directly at the
// store layer — --as's own current password is never asked for, since
// this is the local-store administrative path (the same trust
// boundary as `tickets setup`), not the self-service HTTP route
// (POST /api/v1/accounts/{username}/password) a logged-in human uses
// to change their own.
func runAdminAccountChangePassword(args []string) error {
	fs := flag.NewFlagSet("admin account change-password", flag.ContinueOnError)
	username := fs.String("username", "", "the account whose password to change (required)")
	newPassword := fs.String("new-password", "", "the new password (required)")
	as := fs.String("as", "", "the human account performing this action, e.g. arlo (required)")
	dataDir := fs.String("data-dir", "", "directory for the SQLite database (defaults to the same resolution `tickets server` uses)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *username == "" {
		return fmt.Errorf("admin account change-password: --username is required")
	}
	if *newPassword == "" {
		return fmt.Errorf("admin account change-password: --new-password is required")
	}
	actor, err := parseAsActor(*as)
	if err != nil {
		return fmt.Errorf("admin account change-password: %w", err)
	}

	st, svc, err := openAdminService(*dataDir)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	if err := svc.ChangePassword(context.Background(), service.ChangePasswordRequest{
		Username: *username, NewPassword: *newPassword, SelfService: false,
	}, actor, service.NewCorrelationID()); err != nil {
		return err
	}
	_, err = fmt.Fprintf(os.Stdout, "password changed for %s (all existing sessions invalidated)\n", *username)
	return err
}
