package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
)

// runNotifications is `tickets notifications <subcommand>` — see
// runProject's doc comment for the client-mode convention.
func runNotifications(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("notifications: expected a subcommand (list, read)")
	}
	switch args[0] {
	case "list":
		return runNotificationsList(args[1:])
	case "read":
		return runNotificationsRead(args[1:])
	default:
		return fmt.Errorf("notifications: unknown subcommand %q", args[0])
	}
}

func runNotificationsList(args []string) error {
	fs, cfg, err := newClientFlagSet("notifications list")
	if err != nil {
		return err
	}
	unread := fs.Bool("unread", false, "show only unread notifications")
	limit := fs.Int("limit", 0, "max rows to return (server default 20, max 100)")
	cursor := fs.String("cursor", "", "opaque pagination cursor from a previous call's next_cursor")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	page, err := cfg.newClient().ListNotifications(context.Background(), *unread, *limit, *cursor)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, page)
	}
	rows := make([][]string, len(page.Notifications))
	for i, n := range page.Notifications {
		read := "unread"
		if n.ReadAt != nil {
			read = "read"
		}
		rows[i] = []string{strconv.FormatInt(n.ID, 10), n.Kind, n.Entity, n.TriggeredBy, read}
	}
	if err := writeTable(os.Stdout, []string{"ID", "KIND", "ENTITY", "FROM", "STATUS"}, rows); err != nil {
		return err
	}
	if page.NextCursor != "" {
		_, _ = fmt.Fprintf(os.Stdout, "next_cursor: %s\n", page.NextCursor)
	}
	return nil
}

// runNotificationsRead is `tickets notifications read <id>... [--all]`
// — either one or more notification ids as positional arguments, or
// --all for every currently-unread notification.
func runNotificationsRead(args []string) error {
	fs, cfg, err := newClientFlagSet("notifications read")
	if err != nil {
		return err
	}
	all := fs.Bool("all", false, "mark every currently-unread notification read")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	if !*all && fs.NArg() == 0 {
		return fmt.Errorf("notifications read: expected one or more notification ids, or --all")
	}

	ids := make([]int64, 0, fs.NArg())
	for _, a := range fs.Args() {
		id, err := strconv.ParseInt(a, 10, 64)
		if err != nil {
			return fmt.Errorf("notifications read: %q is not a valid notification id", a)
		}
		ids = append(ids, id)
	}

	marked, err := cfg.newClient().MarkNotificationsRead(context.Background(), ids, *all)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, map[string]int64{"marked": marked})
	}
	_, _ = fmt.Fprintf(os.Stdout, "marked %d notification(s) read\n", marked)
	return nil
}
