package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ArloB/tickets/internal/apiclient"
)

// runFeature is `tickets feature <subcommand>` — see runProject's doc
// comment for the client-mode convention this follows.
func runFeature(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("feature: expected a subcommand (list, get, create, update)")
	}
	switch args[0] {
	case "list":
		return runFeatureList(args[1:])
	case "get":
		return runFeatureGet(args[1:])
	case "create":
		return runFeatureCreate(args[1:])
	case "update":
		return runFeatureUpdate(args[1:])
	default:
		return fmt.Errorf("feature: unknown subcommand %q", args[0])
	}
}

func runFeatureList(args []string) error {
	fs, cfg, err := newClientFlagSet("feature list")
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
		return fmt.Errorf("feature list: --project or TICKETS_PROJECT is required")
	}

	page, err := cfg.newClient().ListFeatures(context.Background(), cfg.Project, *limit, *cursor)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, page)
	}
	enabled := colorEnabled(cfg)
	rows := make([][]string, len(page.Features))
	for i, f := range page.Features {
		rows[i] = []string{f.Ref, f.Title, colorStatus(f.Status, enabled), colorPriority(f.Priority, enabled)}
	}
	if err := writeTable(os.Stdout, []string{"REF", "TITLE", "STATUS", "PRIORITY"}, rows); err != nil {
		return err
	}
	if page.NextCursor != "" {
		_, _ = fmt.Fprintf(os.Stdout, "next_cursor: %s\n", page.NextCursor)
	}
	return nil
}

func runFeatureGet(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("feature get: expected a feature reference as the first argument")
	}
	ref := args[0]
	fs, cfg, err := newClientFlagSet("feature get")
	if err != nil {
		return err
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	f, err := cfg.newClient().GetFeature(context.Background(), ref)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, f)
	}
	enabled := colorEnabled(cfg)
	return writeTable(os.Stdout, []string{"REF", "TITLE", "STATUS", "PRIORITY", "VERSION"},
		[][]string{{f.Ref, f.Title, colorStatus(f.Status, enabled), colorPriority(f.Priority, enabled), fmt.Sprintf("%d", f.Version)}})
}

func runFeatureCreate(args []string) error {
	fs, cfg, err := newClientFlagSet("feature create")
	if err != nil {
		return err
	}
	title := fs.String("title", "", "the feature title (required)")
	description := fs.String("description", "", "optional Markdown description, given inline")
	descriptionFile := fs.String("description-file", "", "path to a file containing the Markdown description, or - for stdin")
	priority := fs.String("priority", "", "critical, high, medium, or low (default medium)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	if cfg.Project == "" {
		return fmt.Errorf("feature create: --project or TICKETS_PROJECT is required")
	}
	set := visitedFlags(fs)
	if !set["title"] {
		return fmt.Errorf("feature create: --title is required")
	}
	if set["description"] && set["description-file"] {
		return fmt.Errorf("feature create: --description and --description-file are mutually exclusive")
	}

	desc := *description
	if set["description-file"] {
		desc, err = readBodyFile(*descriptionFile)
		if err != nil {
			return err
		}
	}

	f, err := cfg.newClient().CreateFeature(context.Background(), cfg.Project, apiclient.CreateFeatureRequest{
		Title: *title, Description: desc, Priority: *priority,
	})
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, f)
	}
	_, err = fmt.Fprintf(os.Stdout, "%s created (version %d)\n", f.Ref, f.Version)
	return err
}

func runFeatureUpdate(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("feature update: expected a feature reference as the first argument")
	}
	ref := args[0]
	fs, cfg, err := newClientFlagSet("feature update")
	if err != nil {
		return err
	}
	title := fs.String("title", "", "the feature's new title (required — full-representation update)")
	description := fs.String("description", "", "the feature's new Markdown description, given inline (defaults to the current description if omitted)")
	descriptionFile := fs.String("description-file", "", "path to a file containing the new Markdown description, or - for stdin")
	priority := fs.String("priority", "", "critical, high, medium, or low (required — full-representation update)")
	ifVersion := fs.Int64("if-version", 0, "the feature's current version, from a prior feature get (required)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	set := visitedFlags(fs)
	if !set["if-version"] {
		return fmt.Errorf("feature update: --if-version is required")
	}
	if !set["title"] || !set["priority"] {
		return fmt.Errorf("feature update: --title and --priority are required (full-representation update)")
	}
	if set["description"] && set["description-file"] {
		return fmt.Errorf("feature update: --description and --description-file are mutually exclusive")
	}

	client := cfg.newClient()
	desc := *description
	switch {
	case set["description-file"]:
		desc, err = readBodyFile(*descriptionFile)
		if err != nil {
			return err
		}
	case !set["description"]:
		// Full-representation update: an omitted field would otherwise be
		// sent as "" and silently wipe the current value server-side.
		current, gerr := client.GetFeature(context.Background(), ref)
		if gerr != nil {
			return gerr
		}
		desc = current.Description
	}

	f, err := client.UpdateFeature(context.Background(), ref, apiclient.UpdateFeatureRequest{
		Title: *title, Description: desc, Priority: *priority,
	}, *ifVersion)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, f)
	}
	_, err = fmt.Fprintf(os.Stdout, "%s updated (version %d)\n", f.Ref, f.Version)
	return err
}
