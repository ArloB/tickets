package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ArloB/tickets/internal/apiclient"
)

// runProject is `tickets project <subcommand>`, the client-mode
// counterpart to `tickets admin`/`tickets setup`: it talks to a
// running Tickets server's HTTP API (internal/apiclient) rather than
// opening internal/store directly.
func runProject(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("project: expected a subcommand (list, create, brief)")
	}
	switch args[0] {
	case "list":
		return runProjectList(args[1:])
	case "create":
		return runProjectCreate(args[1:])
	case "brief":
		return runProjectBrief(args[1:])
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

// runProjectBrief is `tickets project brief KEY` (product spec §12,
// Phase 6 Step 5) — the same orientation read GET /projects/{key}/brief
// and the project_brief MCP tool return.
func runProjectBrief(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("project brief: expected a project key as the first argument")
	}
	key, rest := args[0], args[1:]
	fs, cfg, err := newClientFlagSet("project brief")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	brief, err := cfg.newClient().GetProjectBrief(context.Background(), key)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, brief)
	}

	_, _ = fmt.Fprintf(os.Stdout, "%s — %s (%s)\n\n", brief.Project.Key, brief.Project.Title, brief.Project.Status)

	_, _ = fmt.Fprintln(os.Stdout, "IN PROGRESS / UPCOMING")
	if err := writeTable(os.Stdout, []string{"REF", "TITLE", "STATUS", "PRIORITY"}, ticketBriefRows(brief.InProgress)); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(os.Stdout, "\nISSUE REGISTER")
	if err := writeTable(os.Stdout, []string{"REF", "TITLE", "STATUS", "PRIORITY"}, ticketBriefRows(brief.IssueRegister)); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(os.Stdout, "\nFEATURES")
	featureRows := make([][]string, len(brief.Features))
	for i, f := range brief.Features {
		featureRows[i] = []string{f.Ref, f.Title, f.Status, fmt.Sprintf("%d/%d done", f.TicketsDone, f.TicketsTotal)}
	}
	if err := writeTable(os.Stdout, []string{"REF", "TITLE", "STATUS", "PROGRESS"}, featureRows); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(os.Stdout, "\nRECENT DECISIONS (accepted)")
	decisionRows := make([][]string, len(brief.RecentDecisions))
	for i, d := range brief.RecentDecisions {
		decisionRows[i] = []string{d.Ref, d.Title, d.Status}
	}
	if err := writeTable(os.Stdout, []string{"REF", "TITLE", "STATUS"}, decisionRows); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(os.Stdout, "\nRECENT PLANS")
	planRows := make([][]string, len(brief.RecentPlans))
	for i, p := range brief.RecentPlans {
		planRows[i] = []string{p.Ref, p.Title}
	}
	if err := writeTable(os.Stdout, []string{"REF", "TITLE"}, planRows); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(os.Stdout, "\nRECENT ACTIVITY")
	activityRows := make([][]string, len(brief.RecentActivity))
	for i, e := range brief.RecentActivity {
		activityRows[i] = []string{e.Entity, e.EventType, e.Actor, e.CreatedAt.Format("2006-01-02 15:04")}
	}
	return writeTable(os.Stdout, []string{"ENTITY", "EVENT", "ACTOR", "WHEN"}, activityRows)
}

func ticketBriefRows(tickets []apiclient.TicketCompact) [][]string {
	rows := make([][]string, len(tickets))
	for i, t := range tickets {
		rows[i] = []string{t.Ref, t.Title, t.Status, t.Priority}
	}
	return rows
}
