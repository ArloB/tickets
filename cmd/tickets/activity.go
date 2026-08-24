package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ArloB/tickets/internal/apiclient"
)

// runActivity is `tickets activity <subcommand>` — see runProject's doc
// comment for the client-mode convention this follows. §7.2 gives
// activity no MCP tool, so this list command (plus the web UI) is the
// only agent/script-facing way to read a project's feed.
func runActivity(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("activity: expected a subcommand (list)")
	}
	switch args[0] {
	case "list":
		return runActivityList(args[1:])
	default:
		return fmt.Errorf("activity: unknown subcommand %q", args[0])
	}
}

func runActivityList(args []string) error {
	fs, cfg, err := newClientFlagSet("activity list")
	if err != nil {
		return err
	}
	actor := fs.String("actor", "", "filter to one actor, e.g. human:alice")
	entityKind := fs.String("entity-kind", "", "filter to one entity kind: project, ticket, feature, decision, plan, document")
	eventType := fs.String("event-type", "", "filter to one event type, e.g. ticket_status_changed")
	limit := fs.Int("limit", 0, "max rows to return (server default 20, max 100)")
	cursor := fs.String("cursor", "", "opaque pagination cursor from a previous call's next_cursor")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	if cfg.Project == "" {
		return fmt.Errorf("activity list: --project or TICKETS_PROJECT is required")
	}

	page, err := cfg.newClient().ListActivity(context.Background(), cfg.Project, apiclient.ActivityListOptions{
		Actor: *actor, EntityKind: *entityKind, EventType: *eventType,
	}, *limit, *cursor)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, page)
	}
	rows := make([][]string, len(page.Events))
	for i, e := range page.Events {
		rows[i] = []string{e.CreatedAt.Format("2006-01-02T15:04:05Z"), e.Actor, e.EventType, e.Entity}
	}
	if err := writeTable(os.Stdout, []string{"WHEN", "ACTOR", "EVENT", "ENTITY"}, rows); err != nil {
		return err
	}
	if page.NextCursor != "" {
		_, _ = fmt.Fprintf(os.Stdout, "next_cursor: %s\n", page.NextCursor)
	}
	return nil
}
