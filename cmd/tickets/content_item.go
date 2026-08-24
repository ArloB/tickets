package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ArloB/tickets/internal/apiclient"
)

// contentItemOps bundles the two things that differ between "plan" and
// "document" — everything else about the two subcommands (flags,
// output formatting, full-representation-update handling) is
// identical, so runContentItem is written once here and parameterized
// by this, rather than duplicated across plan.go/document.go. urlKind
// ("plans"/"documents") is passed straight through to apiclient's
// kind-parameterized ContentItem methods — no per-kind closures needed
// now that those methods take urlKind as an argument instead of coming
// in Plan/Document-suffixed pairs.
type contentItemOps struct {
	name    string // "plan" or "document" — used in flag help/error messages
	urlKind string // "plans" or "documents"
}

var planOps = contentItemOps{name: "plan", urlKind: "plans"}
var documentOps = contentItemOps{name: "document", urlKind: "documents"}

// runPlan/runDocument are `tickets plan <subcommand>`/`tickets document
// <subcommand>` — see runDecision's doc comment for the client-mode
// convention this follows.
func runPlan(args []string) error     { return runContentItem(planOps, args) }
func runDocument(args []string) error { return runContentItem(documentOps, args) }

func runContentItem(ops contentItemOps, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%s: expected a subcommand (list, get, create, update, versions, diff)", ops.name)
	}
	switch args[0] {
	case "list":
		return runContentItemList(ops, args[1:])
	case "get":
		return runContentItemGet(ops, args[1:])
	case "create":
		return runContentItemCreate(ops, args[1:])
	case "update":
		return runContentItemUpdate(ops, args[1:])
	case "versions":
		return runContentItemVersions(ops, args[1:])
	case "diff":
		return runContentItemDiff(ops, args[1:])
	default:
		return fmt.Errorf("%s: unknown subcommand %q", ops.name, args[0])
	}
}

func runContentItemList(ops contentItemOps, args []string) error {
	fs, cfg, err := newClientFlagSet(ops.name + " list")
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
	if cfg.Project == "" {
		return fmt.Errorf("%s list: --project or TICKETS_PROJECT is required", ops.name)
	}

	page, err := cfg.newClient().ListContentItems(context.Background(), ops.urlKind, cfg.Project, *limit, *cursor)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, page)
	}
	rows := make([][]string, len(page.Items))
	for i, item := range page.Items {
		rows[i] = []string{item.Ref, item.Title}
	}
	if err := writeTable(os.Stdout, []string{"REF", "TITLE"}, rows); err != nil {
		return err
	}
	if page.NextCursor != "" {
		_, _ = fmt.Fprintf(os.Stdout, "next_cursor: %s\n", page.NextCursor)
	}
	return nil
}

func runContentItemGet(ops contentItemOps, args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("%s get: expected a %s reference as the first argument", ops.name, ops.name)
	}
	ref := args[0]
	fs, cfg, err := newClientFlagSet(ops.name + " get")
	if err != nil {
		return err
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	item, err := cfg.newClient().GetContentItem(context.Background(), ops.urlKind, ref)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, item)
	}
	return writeTable(os.Stdout, []string{"REF", "TITLE", "VERSION"},
		[][]string{{item.Ref, item.Title, fmt.Sprintf("%d", item.Version)}})
}

func runContentItemCreate(ops contentItemOps, args []string) error {
	fs, cfg, err := newClientFlagSet(ops.name + " create")
	if err != nil {
		return err
	}
	title := fs.String("title", "", "the "+ops.name+"'s title (required)")
	body := fs.String("body", "", "Markdown body, given inline")
	bodyFile := fs.String("body-file", "", "path to a file containing the Markdown body, or - for stdin")
	idempotencyKey := fs.String("idempotency-key", "", "optional: a client-chosen key that makes a retried call safe — reusing the same key with the same content returns the original "+ops.name+" instead of creating a duplicate")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	if cfg.Project == "" {
		return fmt.Errorf("%s create: --project or TICKETS_PROJECT is required", ops.name)
	}
	set := visitedFlags(fs)
	if !set["title"] {
		return fmt.Errorf("%s create: --title is required", ops.name)
	}
	if set["body"] && set["body-file"] {
		return fmt.Errorf("%s create: --body and --body-file are mutually exclusive", ops.name)
	}
	resolvedBody, err := resolveTextFlag(*body, *bodyFile, set["body-file"])
	if err != nil {
		return err
	}

	item, err := cfg.newClient().CreateContentItem(context.Background(), ops.urlKind, cfg.Project, apiclient.CreateContentItemRequest{
		Title: *title, Body: resolvedBody,
	}, *idempotencyKey)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, item)
	}
	_, err = fmt.Fprintf(os.Stdout, "%s created (version %d)\n", item.Ref, item.Version)
	return err
}

func runContentItemUpdate(ops contentItemOps, args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("%s update: expected a %s reference as the first argument", ops.name, ops.name)
	}
	ref := args[0]
	fs, cfg, err := newClientFlagSet(ops.name + " update")
	if err != nil {
		return err
	}
	title := fs.String("title", "", "the "+ops.name+"'s new title (required — full-representation update)")
	body := fs.String("body", "", "the "+ops.name+"'s new Markdown body, given inline (defaults to the current body if omitted)")
	bodyFile := fs.String("body-file", "", "path to a file containing the new Markdown body, or - for stdin")
	ifVersion := fs.Int64("if-version", 0, "the "+ops.name+"'s current version, from a prior "+ops.name+" get (required)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	set := visitedFlags(fs)
	if !set["if-version"] {
		return fmt.Errorf("%s update: --if-version is required", ops.name)
	}
	if !set["title"] {
		return fmt.Errorf("%s update: --title is required (full-representation update)", ops.name)
	}
	if set["body"] && set["body-file"] {
		return fmt.Errorf("%s update: --body and --body-file are mutually exclusive", ops.name)
	}

	client := cfg.newClient()
	var current apiclient.ContentItem
	if !set["body"] && !set["body-file"] {
		// Full-representation update: an unset body would otherwise be
		// sent as "" and silently wipe the current body server-side.
		current, err = client.GetContentItem(context.Background(), ops.urlKind, ref)
		if err != nil {
			return err
		}
	}
	resolvedBody, err := resolveTextFlagOr(*body, *bodyFile, set["body"], set["body-file"], current.Body)
	if err != nil {
		return err
	}

	item, err := client.UpdateContentItem(context.Background(), ops.urlKind, ref, apiclient.UpdateContentItemRequest{
		Title: *title, Body: resolvedBody,
	}, *ifVersion)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, item)
	}
	_, err = fmt.Fprintf(os.Stdout, "%s updated (version %d)\n", item.Ref, item.Version)
	return err
}

func runContentItemVersions(ops contentItemOps, args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("%s versions: expected a %s reference as the first argument", ops.name, ops.name)
	}
	ref := args[0]
	fs, cfg, err := newClientFlagSet(ops.name + " versions")
	if err != nil {
		return err
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	page, err := cfg.newClient().ListContentItemVersions(context.Background(), ops.urlKind, ref)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, page)
	}
	rows := make([][]string, len(page.Versions))
	for i, v := range page.Versions {
		rows[i] = []string{fmt.Sprintf("%d", v.Version), v.Title, v.EditedBy, v.CreatedAt.Format("2006-01-02T15:04:05Z")}
	}
	return writeTable(os.Stdout, []string{"VERSION", "TITLE", "EDITED_BY", "CREATED_AT"}, rows)
}

func runContentItemDiff(ops contentItemOps, args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("%s diff: expected a %s reference as the first argument", ops.name, ops.name)
	}
	ref := args[0]
	fs, cfg, err := newClientFlagSet(ops.name + " diff")
	if err != nil {
		return err
	}
	from := fs.Int64("from", 0, "the earlier version number to compare (required)")
	to := fs.Int64("to", 0, "the later version number to compare (required)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	set := visitedFlags(fs)
	if !set["from"] || !set["to"] {
		return fmt.Errorf("%s diff: --from and --to are required", ops.name)
	}

	diff, err := cfg.newClient().GetContentItemDiff(context.Background(), ops.urlKind, ref, *from, *to)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, diff)
	}
	fields := []struct {
		name  string
		lines []apiclient.DiffLine
	}{
		{"title", diff.Title},
		{"body", diff.Body},
	}
	for _, f := range fields {
		for _, line := range f.lines {
			prefix := " "
			switch line.Op {
			case "add":
				prefix = "+"
			case "remove":
				prefix = "-"
			}
			_, _ = fmt.Fprintf(os.Stdout, "%s %s| %s%s\n", prefix, f.name, prefix, line.Text)
		}
	}
	return nil
}
