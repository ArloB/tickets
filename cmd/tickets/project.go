package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/ArloB/tickets/internal/apiclient"
)

// runProject is `tickets project <subcommand>`, the client-mode
// counterpart to `tickets admin`/`tickets setup`: it talks to a
// running Tickets server's HTTP API (internal/apiclient) rather than
// opening internal/store directly.
func runProject(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("project: expected a subcommand (list, create)")
	}
	switch args[0] {
	case "list":
		return runProjectList(args[1:])
	case "create":
		return runProjectCreate(args[1:])
	default:
		return fmt.Errorf("project: unknown subcommand %q", args[0])
	}
}

func runProjectCreate(args []string) error {
	fs, cfg, err := newClientFlagSet("project create")
	if err != nil {
		return err
	}
	key := fs.String("key", "", "the project key, e.g. ABC: uppercase letters/digits, 2-10 characters, starting with a letter (required)")
	title := fs.String("title", "", "the project title (required)")
	description := fs.String("description", "", "optional Markdown description, given inline")
	descriptionFile := fs.String("description-file", "", "path to a file containing the Markdown description, or - for stdin")
	idempotencyKey := fs.String("idempotency-key", "", "optional: a client-chosen key that makes a retried call safe — reusing the same key with the same content returns the original project instead of creating a duplicate")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	set := visitedFlags(fs)
	if !set["key"] {
		return fmt.Errorf("project create: --key is required")
	}
	if !set["title"] {
		return fmt.Errorf("project create: --title is required")
	}
	if set["description"] && set["description-file"] {
		return fmt.Errorf("project create: --description and --description-file are mutually exclusive")
	}

	desc := *description
	if set["description-file"] {
		desc, err = readBodyFile(*descriptionFile)
		if err != nil {
			return err
		}
	}

	proj, err := cfg.newClient().CreateProject(context.Background(), apiclient.CreateProjectRequest{
		Key: *key, Title: *title, Description: desc,
	}, *idempotencyKey)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, proj)
	}
	_, err = fmt.Fprintf(os.Stdout, "%s created (version %d)\n", proj.Key, proj.Version)
	return err
}

func runProjectList(args []string) error {
	fs, cfg, err := newClientFlagSet("project list")
	if err != nil {
		return err
	}
	limit := fs.Int("limit", 0, "max rows to return (server default 20, max 100)")
	cursor := fs.String("cursor", "", "opaque pagination cursor from a previous call's next_cursor")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	page, err := cfg.newClient().ListProjects(context.Background(), *limit, *cursor)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, page)
	}

	rows := make([][]string, len(page.Projects))
	for i, p := range page.Projects {
		rows[i] = []string{p.Key, p.Title, p.Status, strconv.FormatInt(p.Version, 10)}
	}
	if err := writeTable(os.Stdout, []string{"KEY", "TITLE", "STATUS", "VERSION"}, rows); err != nil {
		return err
	}
	if page.NextCursor != "" {
		_, _ = fmt.Fprintf(os.Stdout, "next_cursor: %s\n", page.NextCursor)
	}
	return nil
}
