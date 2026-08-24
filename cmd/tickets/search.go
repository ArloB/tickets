package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ArloB/tickets/internal/apiclient"
)

// runSearch is `tickets search <query> [flags]` — a flat command, not
// a `list`-subcommand family like activity/project, since search has
// exactly one operation (§5.12 names no create/update/delete for a
// search index — it's a read-only view derived from other records).
func runSearch(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("search: expected a query as the first argument, e.g. `tickets search \"reticulate splines\"`")
	}
	query := args[0]
	fs, cfg, err := newClientFlagSet("search")
	if err != nil {
		return err
	}
	kind := fs.String("kind", "", "comma-separated kinds to filter to: ticket,feature,decision,plan,document,comment,attachment,link")
	status := fs.String("status", "", "filter to one status value (workflow status for tickets/features, decision status for decisions)")
	limit := fs.Int("limit", 0, "max rows to return (server default 20, max 100)")
	cursor := fs.String("cursor", "", "opaque pagination cursor from a previous call's next_cursor")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	var kinds []string
	if *kind != "" {
		kinds = strings.Split(*kind, ",")
	}

	page, err := cfg.newClient().Search(context.Background(), query, apiclient.SearchOptions{
		Project: cfg.Project, Kinds: kinds, Status: *status,
	}, *limit, *cursor)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, page)
	}
	rows := make([][]string, len(page.Hits))
	for i, h := range page.Hits {
		rows[i] = []string{h.Kind, h.Ref, h.Title, h.Snippet}
	}
	if err := writeTable(os.Stdout, []string{"KIND", "REF", "TITLE", "SNIPPET"}, rows); err != nil {
		return err
	}
	if page.NextCursor != "" {
		_, _ = fmt.Fprintf(os.Stdout, "next_cursor: %s\n", page.NextCursor)
	}
	return nil
}
