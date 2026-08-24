package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/ArloB/tickets/internal/config"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
)

// runSetup is `tickets setup`: first-run administrative setup (product
// spec §6.5's "first-run setup and sign-in"). It creates the single
// admin account service.CreateAdminAccount allows — and only that —
// refusing clearly if a human account already exists, so setup is
// safe to run at most once per installation.
//
// Non-interactive by construction (product spec §7.3: "prompts are
// opt-in and never appear when stdin is not a terminal" — this command
// simply never prompts at all, the strictest reading of that rule):
// credentials come from --username/--password flags or
// TICKETS_ADMIN_USERNAME/TICKETS_ADMIN_PASSWORD environment variables.
// It opens internal/store directly rather than talking to a running
// server's HTTP API, the same way runServer does — full CLI
// ergonomics (config file/env precedence for every setting, a client
// mode that talks to a remote server) are Phase 3 work.
func runSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	username := fs.String("username", envOr("TICKETS_ADMIN_USERNAME", ""), "admin account username")
	password := fs.String("password", envOr("TICKETS_ADMIN_PASSWORD", ""), "admin account password")
	dataDir := fs.String("data-dir", "", "directory for the SQLite database (defaults to the same resolution `tickets server` uses)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *username == "" {
		return fmt.Errorf("setup: --username or TICKETS_ADMIN_USERNAME is required (setup never prompts)")
	}
	if *password == "" {
		return fmt.Errorf("setup: --password or TICKETS_ADMIN_PASSWORD is required (setup never prompts)")
	}

	var cfgArgs []string
	if *dataDir != "" {
		cfgArgs = []string{"--data-dir", *dataDir}
	}
	cfg, err := config.Load(cfgArgs)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open store at %s: %w", cfg.DataDir, err)
	}
	defer func() { _ = st.Close() }()

	svc := service.New(st, nil)
	actor, err := svc.CreateAdminAccount(context.Background(), *username, *password)
	if err != nil {
		return fmt.Errorf("create admin account: %w", err)
	}

	_, _ = fmt.Fprintf(os.Stdout, "created admin account %s\n", actor)
	_, _ = fmt.Fprintln(os.Stdout, "the password you provided was not echoed back or logged; store it somewhere safe")
	return nil
}
