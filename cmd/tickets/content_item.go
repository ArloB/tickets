package main

import (
	"context"
	"fmt"
	"io"
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
		return fmt.Errorf("%s: expected a subcommand (list, get, create, update, archive, unarchive, versions, diff, download)", ops.name)
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
	case "archive":
		return runContentItemSetStatus(ops, args[1:], "archived")
	case "unarchive":
		return runContentItemSetStatus(ops, args[1:], "active")
	case "versions":
		return runContentItemVersions(ops, args[1:])
	case "diff":
		return runContentItemDiff(ops, args[1:])
	case "download":
		return runContentItemDownload(ops, args[1:])
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
	includeArchived := fs.Bool("include-archived", false, "also list archived "+ops.name+"s (default: active only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	if cfg.Project == "" {
		return fmt.Errorf("%s list: --project or TICKETS_PROJECT is required", ops.name)
	}

	page, err := cfg.newClient().ListContentItems(context.Background(), ops.urlKind, cfg.Project, *limit, *cursor, *includeArchived)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, page)
	}
	rows := make([][]string, len(page.Items))
	for i, item := range page.Items {
		rows[i] = []string{item.Ref, item.Title, item.Status}
	}
	if err := writeTable(os.Stdout, []string{"REF", "TITLE", "STATUS"}, rows); err != nil {
		return err
	}
	if page.NextCursor != "" {
		_, _ = fmt.Fprintf(os.Stdout, "next_cursor: %s\n", page.NextCursor)
	}
	return nil
}

// runContentItemSetStatus is `tickets plan|document archive`/`...
// unarchive REF`, mirroring runProjectSetStatus.
func runContentItemSetStatus(ops contentItemOps, args []string, status string) error {
	verb := "archive"
	if status == "active" {
		verb = "unarchive"
	}
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("%s %s: expected a %s reference as the first argument", ops.name, verb, ops.name)
	}
	ref := args[0]
	fs, cfg, err := newClientFlagSet(ops.name + " " + verb)
	if err != nil {
		return err
	}
	ifVersion := fs.Int64("if-version", 0, "the "+ops.name+"'s current version, from a prior "+ops.name+" get (required; for an archived "+ops.name+", use "+ops.name+" list --include-archived)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	if !visitedFlags(fs)["if-version"] {
		return fmt.Errorf("%s %s: --if-version is required", ops.name, verb)
	}

	item, err := cfg.newClient().SetContentItemStatus(context.Background(), ops.urlKind, ref, status, *ifVersion)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, item)
	}
	_, err = fmt.Fprintf(os.Stdout, "%s now %s (version %d)\n", item.Ref, item.Status, item.Version)
	return err
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
	return writeTable(os.Stdout, []string{"REF", "TITLE", "STATUS", "VERSION"},
		[][]string{{item.Ref, item.Title, item.Status, fmt.Sprintf("%d", item.Version)}})
}

func runContentItemCreate(ops contentItemOps, args []string) error {
	fs, cfg, err := newClientFlagSet(ops.name + " create")
	if err != nil {
		return err
	}
	title := fs.String("title", "", "the "+ops.name+"'s title (required)")
	body := fs.String("body", "", "Markdown body, given inline (representation=markdown, the default)")
	bodyFile := fs.String("body-file", "", "path to a file containing the Markdown body, or - for stdin")
	file := fs.String("file", "", "path to a file to upload (representation=file)")
	path := fs.String("path", "", "a path reference, never read by the server (representation=path)")
	contentURL := fs.String("content-url", "", "an external URL (representation=url)")
	mediaType := fs.String("media-type", "", "MIME type for a file representation (defaults to the file's own detected type if omitted)")
	idempotencyKey := fs.String("idempotency-key", "", "optional: a client-chosen key that makes a retried call safe — reusing the same key with the same content returns the original "+ops.name+" instead of creating a duplicate (ignored for --file, which can't be fingerprinted)")
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
	representationFlags := 0
	for _, s := range []bool{set["body"] || set["body-file"], set["file"], set["path"], set["content-url"]} {
		if s {
			representationFlags++
		}
	}
	if representationFlags > 1 {
		return fmt.Errorf("%s create: --body/--body-file, --file, --path, and --url are mutually exclusive", ops.name)
	}

	client := cfg.newClient()
	var item apiclient.ContentItem
	switch {
	case set["file"]:
		f, ferr := os.Open(*file)
		if ferr != nil {
			return fmt.Errorf("%s create: open %s: %w", ops.name, *file, ferr)
		}
		defer func() { _ = f.Close() }()
		item, err = client.UploadContentItem(context.Background(), ops.urlKind, cfg.Project, *title, fileBaseName(*file), *mediaType, f)
	case set["path"]:
		item, err = client.CreateContentItem(context.Background(), ops.urlKind, cfg.Project, apiclient.CreateContentItemRequest{
			Title: *title, Representation: "path", Path: *path,
		}, *idempotencyKey)
	case set["content-url"]:
		item, err = client.CreateContentItem(context.Background(), ops.urlKind, cfg.Project, apiclient.CreateContentItemRequest{
			Title: *title, Representation: "url", URL: *contentURL,
		}, *idempotencyKey)
	default:
		var resolvedBody string
		resolvedBody, err = resolveTextFlag(*body, *bodyFile, set["body-file"])
		if err != nil {
			return err
		}
		item, err = client.CreateContentItem(context.Background(), ops.urlKind, cfg.Project, apiclient.CreateContentItemRequest{
			Title: *title, Representation: "markdown", Body: resolvedBody,
		}, *idempotencyKey)
	}
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
	body := fs.String("body", "", "the "+ops.name+"'s new Markdown body, given inline (representation=markdown only; defaults to the current body if omitted)")
	bodyFile := fs.String("body-file", "", "path to a file containing the new Markdown body, or - for stdin")
	file := fs.String("file", "", "path to a new file to upload as the next version (representation=file only)")
	path := fs.String("path", "", "the item's new path value (representation=path only)")
	contentURL := fs.String("content-url", "", "the item's new URL value (representation=url only)")
	mediaType := fs.String("media-type", "", "MIME type for a file representation (defaults to the file's own detected type if omitted)")
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
	var item apiclient.ContentItem
	switch {
	case set["file"]:
		f, ferr := os.Open(*file)
		if ferr != nil {
			return fmt.Errorf("%s update: open %s: %w", ops.name, *file, ferr)
		}
		defer func() { _ = f.Close() }()
		item, err = client.ReplaceContentItemFile(context.Background(), ops.urlKind, ref, *title, fileBaseName(*file), *mediaType, f, *ifVersion)
	case set["path"]:
		item, err = client.UpdateContentItem(context.Background(), ops.urlKind, ref, apiclient.UpdateContentItemRequest{
			Title: *title, Path: *path,
		}, *ifVersion)
	case set["content-url"]:
		item, err = client.UpdateContentItem(context.Background(), ops.urlKind, ref, apiclient.UpdateContentItemRequest{
			Title: *title, URL: *contentURL,
		}, *ifVersion)
	default:
		var current apiclient.ContentItem
		if !set["body"] && !set["body-file"] {
			// Full-representation update: an unset body would otherwise be
			// sent as "" and silently wipe the current body server-side.
			current, err = client.GetContentItem(context.Background(), ops.urlKind, ref)
			if err != nil {
				return err
			}
		}
		var resolvedBody string
		resolvedBody, err = resolveTextFlagOr(*body, *bodyFile, set["body"], set["body-file"], current.Body)
		if err != nil {
			return err
		}
		item, err = client.UpdateContentItem(context.Background(), ops.urlKind, ref, apiclient.UpdateContentItemRequest{
			Title: *title, Body: resolvedBody,
		}, *ifVersion)
	}
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

func runContentItemDownload(ops contentItemOps, args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("%s download: expected a %s reference as the first argument", ops.name, ops.name)
	}
	ref := args[0]
	fs, cfg, err := newClientFlagSet(ops.name + " download")
	if err != nil {
		return err
	}
	version := fs.Int64("version", 0, "download this archived version instead of the current one")
	output := fs.String("output", "", "write to this file instead of stdout")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	client := cfg.newClient()
	var dl apiclient.ContentItemDownload
	if *version > 0 {
		dl, err = client.DownloadContentItemVersion(context.Background(), ops.urlKind, ref, *version)
	} else {
		dl, err = client.DownloadContentItem(context.Background(), ops.urlKind, ref)
	}
	if err != nil {
		return err
	}
	defer func() { _ = dl.Content.Close() }()

	out := os.Stdout
	if *output != "" {
		f, ferr := os.Create(*output)
		if ferr != nil {
			return fmt.Errorf("%s download: create %s: %w", ops.name, *output, ferr)
		}
		defer func() { _ = f.Close() }()
		out = f
	}
	if _, err := io.Copy(out, dl.Content); err != nil {
		return fmt.Errorf("%s download: write output: %w", ops.name, err)
	}
	return nil
}
