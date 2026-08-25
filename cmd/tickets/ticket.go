package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ArloB/tickets/internal/apiclient"
)

// runTicket is `tickets ticket <subcommand>` — see runProject's doc
// comment for the client-mode convention this follows. Every
// subcommand that targets one ticket takes its reference as the first
// positional argument, before any flags (`tickets ticket update
// ABC-123 --status in_progress`) — stdlib flag stops parsing at the
// first non-flag argument, so the reference can't be interspersed
// after flags the way it can with some CLI frameworks.
func runTicket(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("ticket: expected a subcommand (list, get, create, update, assign, move, delete, restore, relate, relationships, unrelate, associate, associations, disassociate)")
	}
	switch args[0] {
	case "list":
		return runTicketList(args[1:])
	case "get":
		return runTicketGet(args[1:])
	case "create":
		return runTicketCreate(args[1:])
	case "update":
		return runTicketUpdate(args[1:])
	case "assign":
		return runTicketAssign(args[1:])
	case "move":
		return runTicketMove(args[1:])
	case "delete":
		return runTicketDelete(args[1:])
	case "restore":
		return runTicketRestore(args[1:])
	case "relate":
		return runTicketRelate(args[1:])
	case "relationships":
		return runTicketRelationships(args[1:])
	case "unrelate":
		return runTicketUnrelate(args[1:])
	case "associate":
		return runTicketAssociate(args[1:])
	case "associations":
		return runTicketAssociations(args[1:])
	case "disassociate":
		return runTicketDisassociate(args[1:])
	default:
		return fmt.Errorf("ticket: unknown subcommand %q", args[0])
	}
}

func runTicketList(args []string) error {
	fs, cfg, err := newClientFlagSet("ticket list")
	if err != nil {
		return err
	}
	view := fs.String("view", "priority_queue", "priority_queue or issue_register")
	limit := fs.Int("limit", 0, "max rows to return (server default 20, max 100)")
	cursor := fs.String("cursor", "", "opaque pagination cursor from a previous call's next_cursor")
	fields := fs.String("fields", "", "comma-separated subset of ticket fields to return, e.g. ref,title,status")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	if cfg.Project == "" {
		return fmt.Errorf("ticket list: --project or TICKETS_PROJECT is required")
	}

	if *fields != "" {
		page, err := cfg.newClient().ListTicketsFields(context.Background(), cfg.Project, *view, *limit, *cursor, splitCommaList(*fields))
		if err != nil {
			return err
		}
		if cfg.JSON {
			return writeJSON(os.Stdout, page)
		}
		return writeProjectedRows(os.Stdout, splitCommaList(*fields), page.Tickets, page.NextCursor)
	}

	page, err := cfg.newClient().ListTickets(context.Background(), cfg.Project, *view, *limit, *cursor)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, page)
	}

	enabled := colorEnabled(cfg)
	rows := make([][]string, len(page.Tickets))
	for i, t := range page.Tickets {
		severity := ""
		if t.Severity != nil {
			severity = colorPriority(*t.Severity, enabled)
		}
		rows[i] = []string{t.Ref, t.Title, t.Type, colorStatus(t.Status, enabled), colorPriority(t.Priority, enabled), severity}
	}
	if err := writeTable(os.Stdout, []string{"REF", "TITLE", "TYPE", "STATUS", "PRIORITY", "SEVERITY"}, rows); err != nil {
		return err
	}
	if page.NextCursor != "" {
		_, _ = fmt.Fprintf(os.Stdout, "next_cursor: %s\n", page.NextCursor)
	}
	return nil
}

// runTicketGet is `tickets ticket get <ref>` — GET /tickets/{ref},
// supporting ?fields=/?include= (docs/contracts/representations.md),
// the only ticket subcommand either flag can attach to (feature/decision
// get don't support projection today — only the ticket endpoints do).
func runTicketGet(args []string) error {
	ref, rest, err := popTicketRef("ticket get", args)
	if err != nil {
		return err
	}
	fs, cfg, err := newClientFlagSet("ticket get")
	if err != nil {
		return err
	}
	fields := fs.String("fields", "", "comma-separated subset of ticket fields to return, e.g. ref,title,status")
	include := fs.String("include", "", "comma-separated sub-resources to expand: comments, relationships")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	// Unlike --fields (validated server-side, see internal/httpapi/
	// representation.go's validateFieldNames), an unknown --include name
	// isn't rejected by the server — getTicket in internal/httpapi/
	// tickets.go just checks include["comments"]/include["relationships"]
	// and silently no-ops on anything else, so a typo would otherwise
	// exit 0 with no comments key: indistinguishable from a ticket that
	// genuinely has none. Checked here instead. If a later phase adds a
	// third includable sub-resource, both this list and getTicket's
	// checks need updating together.
	includeNames := splitCommaList(*include)
	for _, name := range includeNames {
		if name != "comments" && name != "relationships" {
			return fmt.Errorf("ticket get: --include %q is not a recognized sub-resource (comments, relationships)", name)
		}
	}

	if *fields != "" || *include != "" {
		out, err := cfg.newClient().GetTicketFields(context.Background(), ref, splitCommaList(*fields), includeNames)
		if err != nil {
			return err
		}
		if cfg.JSON {
			return writeJSON(os.Stdout, out)
		}
		if *fields != "" {
			return writeProjectedRow(os.Stdout, splitCommaList(*fields), out)
		}
		return writeJSON(os.Stdout, out)
	}

	t, err := cfg.newClient().GetTicket(context.Background(), ref)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, t)
	}
	enabled := colorEnabled(cfg)
	severity := ""
	if t.Severity != nil {
		severity = colorPriority(*t.Severity, enabled)
	}
	return writeTable(os.Stdout, []string{"REF", "TITLE", "TYPE", "STATUS", "PRIORITY", "SEVERITY", "VERSION"},
		[][]string{{t.Ref, t.Title, t.Type, colorStatus(t.Status, enabled), colorPriority(t.Priority, enabled), severity, fmt.Sprintf("%d", t.Version)}})
}

// runTicketCreate is `tickets ticket create` — POST /projects/{key}/tickets.
// Closes a real CLI/MCP parity gap found during Phase 6 Step 9
// documentation (docs/mvp-acceptance.md row 10): every other principal
// entity (project, feature, decision, plan, document) already has a
// CLI create subcommand and MCP has ticket_create, but until this
// change a ticket could only be created via the web UI, a raw HTTP
// call, or MCP — not the CLI JSON path §16 criterion 10 promises.
// No --idempotency-key here: internal/apiclient.CreateTicket doesn't
// forward one today (unlike CreateProject/CreateDecision/AddComment),
// consistent with docs/contracts/cli.md's "No other create command
// exposes this flag today."
func runTicketCreate(args []string) error {
	fs, cfg, err := newClientFlagSet("ticket create")
	if err != nil {
		return err
	}
	ticketType := fs.String("type", "", "task, bug, security, or chore (required)")
	title := fs.String("title", "", "the ticket title (required)")
	description := fs.String("description", "", "optional Markdown description, given inline")
	descriptionFile := fs.String("description-file", "", "path to a file containing the Markdown description, or - for stdin")
	priority := fs.String("priority", "", "critical, high, medium, or low (default medium)")
	severity := fs.String("severity", "", "critical, high, medium, or low (bug/security tickets only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	if cfg.Project == "" {
		return fmt.Errorf("ticket create: --project or TICKETS_PROJECT is required")
	}
	set := visitedFlags(fs)
	if !set["type"] {
		return fmt.Errorf("ticket create: --type is required")
	}
	if !set["title"] {
		return fmt.Errorf("ticket create: --title is required")
	}
	if set["description"] && set["description-file"] {
		return fmt.Errorf("ticket create: --description and --description-file are mutually exclusive")
	}

	desc := *description
	if set["description-file"] {
		desc, err = readBodyFile(*descriptionFile)
		if err != nil {
			return err
		}
	}

	t, err := cfg.newClient().CreateTicket(context.Background(), cfg.Project, apiclient.CreateTicketRequest{
		Type: *ticketType, Title: *title, Description: desc, Priority: *priority, Severity: *severity,
	})
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, t)
	}
	_, err = fmt.Fprintf(os.Stdout, "%s created (version %d)\n", t.Ref, t.Version)
	return err
}

// popTicketRef extracts a subcommand's leading positional ticket
// reference, returning the remaining args to hand to fs.Parse.
func popTicketRef(cmd string, args []string) (ref string, rest []string, err error) {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return "", nil, fmt.Errorf("%s: expected a ticket reference as the first argument", cmd)
	}
	return args[0], args[1:], nil
}

// visitedFlags returns the set of flag names fs.Parse actually saw on
// the command line — used to distinguish "the caller explicitly set
// this" from "this is just the flag's zero-value default," which a
// plain *string/*int64 from fs.String/fs.Int64 can't tell apart on its
// own.
func visitedFlags(fs *flag.FlagSet) map[string]bool {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return set
}

func runTicketUpdate(args []string) error {
	ref, rest, err := popTicketRef("ticket update", args)
	if err != nil {
		return err
	}
	fs, cfg, err := newClientFlagSet("ticket update")
	if err != nil {
		return err
	}
	status := fs.String("status", "", "new workflow status: backlog, ready, in_progress, blocked, review, done, or cancelled")
	ticketType := fs.String("type", "", "task, bug, security, or chore")
	title := fs.String("title", "", "new title")
	description := fs.String("description", "", "new Markdown description, given inline")
	descriptionFile := fs.String("description-file", "", "path to a file containing the new Markdown description, or - for stdin")
	priority := fs.String("priority", "", "critical, high, medium, or low")
	severity := fs.String("severity", "", "critical, high, medium, or low (bug/security tickets only)")
	ifVersion := fs.Int64("if-version", 0, "the ticket's current version, from a prior ticket get (required)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	set := visitedFlags(fs)
	if !set["if-version"] {
		return fmt.Errorf("ticket update: --if-version is required")
	}
	if set["description"] && set["description-file"] {
		return fmt.Errorf("ticket update: --description and --description-file are mutually exclusive")
	}

	opts := apiclient.UpdateTicketOptions{ExpectedVersion: ifVersion}
	if set["status"] {
		opts.Status = status
	}
	if set["type"] {
		opts.Type = ticketType
	}
	if set["title"] {
		opts.Title = title
	}
	if set["description"] {
		opts.Description = description
	}
	if set["description-file"] {
		d, err := readBodyFile(*descriptionFile)
		if err != nil {
			return err
		}
		opts.Description = &d
	}
	if set["priority"] {
		opts.Priority = priority
	}
	if set["severity"] {
		opts.Severity = severity
	}

	ticket, err := cfg.newClient().UpdateTicket(context.Background(), ref, opts)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, ticket)
	}
	_, err = fmt.Fprintf(os.Stdout, "%s updated (version %d)\n", ticket.Ref, ticket.Version)
	return err
}

func runTicketAssign(args []string) error {
	ref, rest, err := popTicketRef("ticket assign", args)
	if err != nil {
		return err
	}
	fs, cfg, err := newClientFlagSet("ticket assign")
	if err != nil {
		return err
	}
	assignee := fs.String("assignee", "", "actor to assign, as kind:name (e.g. agent:codex or human:alice)")
	unassign := fs.Bool("unassign", false, "clear the ticket's assignee")
	ifVersion := fs.Int64("if-version", 0, "the ticket's current version, from a prior ticket get (required)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	set := visitedFlags(fs)
	if !set["if-version"] {
		return fmt.Errorf("ticket assign: --if-version is required")
	}
	if *unassign == set["assignee"] {
		return fmt.Errorf("ticket assign: exactly one of --assignee or --unassign is required")
	}

	var assigneePtr *string
	if set["assignee"] {
		assigneePtr = assignee
	}
	ticket, err := cfg.newClient().AssignTicket(context.Background(), ref, assigneePtr, *ifVersion)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, ticket)
	}
	_, err = fmt.Fprintf(os.Stdout, "%s assignee updated (version %d)\n", ticket.Ref, ticket.Version)
	return err
}

func runTicketMove(args []string) error {
	ref, rest, err := popTicketRef("ticket move", args)
	if err != nil {
		return err
	}
	fs, cfg, err := newClientFlagSet("ticket move")
	if err != nil {
		return err
	}
	feature := fs.String("feature", "", "the destination feature reference, e.g. ABC-F2 (required)")
	ifVersion := fs.Int64("if-version", 0, "the ticket's current version, from a prior ticket get (required)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	set := visitedFlags(fs)
	if !set["if-version"] {
		return fmt.Errorf("ticket move: --if-version is required")
	}
	if !set["feature"] {
		return fmt.Errorf("ticket move: --feature is required")
	}

	ticket, err := cfg.newClient().MoveTicket(context.Background(), ref, *feature, *ifVersion)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, ticket)
	}
	_, err = fmt.Fprintf(os.Stdout, "%s moved to %s (version %d)\n", ticket.Ref, ticket.Feature, ticket.Version)
	return err
}

func runTicketDelete(args []string) error {
	ref, rest, err := popTicketRef("ticket delete", args)
	if err != nil {
		return err
	}
	fs, cfg, err := newClientFlagSet("ticket delete")
	if err != nil {
		return err
	}
	ifVersion := fs.Int64("if-version", 0, "the ticket's current version, from a prior ticket get (required)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	if !visitedFlags(fs)["if-version"] {
		return fmt.Errorf("ticket delete: --if-version is required")
	}

	newVersion, err := cfg.newClient().DeleteTicket(context.Background(), ref, *ifVersion)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, map[string]any{"ref": ref, "version": newVersion})
	}
	_, err = fmt.Fprintf(os.Stdout, "%s deleted (version %d)\n", ref, newVersion)
	return err
}

func runTicketRestore(args []string) error {
	ref, rest, err := popTicketRef("ticket restore", args)
	if err != nil {
		return err
	}
	fs, cfg, err := newClientFlagSet("ticket restore")
	if err != nil {
		return err
	}
	ifVersion := fs.Int64("if-version", 0, "the ticket's version at delete time (required)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	if !visitedFlags(fs)["if-version"] {
		return fmt.Errorf("ticket restore: --if-version is required")
	}

	ticket, err := cfg.newClient().RestoreTicket(context.Background(), ref, *ifVersion)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, ticket)
	}
	_, err = fmt.Fprintf(os.Stdout, "%s restored (version %d)\n", ticket.Ref, ticket.Version)
	return err
}
