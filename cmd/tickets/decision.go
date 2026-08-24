package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ArloB/tickets/internal/apiclient"
)

// runDecision is `tickets decision <subcommand>` — see runProject's
// doc comment for the client-mode convention this follows.
func runDecision(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("decision: expected a subcommand (list, get, create, update, versions, diff)")
	}
	switch args[0] {
	case "list":
		return runDecisionList(args[1:])
	case "get":
		return runDecisionGet(args[1:])
	case "create":
		return runDecisionCreate(args[1:])
	case "update":
		return runDecisionUpdate(args[1:])
	case "versions":
		return runDecisionVersions(args[1:])
	case "diff":
		return runDecisionDiff(args[1:])
	default:
		return fmt.Errorf("decision: unknown subcommand %q", args[0])
	}
}

func runDecisionList(args []string) error {
	fs, cfg, err := newClientFlagSet("decision list")
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
		return fmt.Errorf("decision list: --project or TICKETS_PROJECT is required")
	}

	page, err := cfg.newClient().ListDecisions(context.Background(), cfg.Project, *limit, *cursor)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, page)
	}
	enabled := colorEnabled(cfg)
	rows := make([][]string, len(page.Decisions))
	for i, d := range page.Decisions {
		rows[i] = []string{d.Ref, d.Title, colorStatus(d.Status, enabled)}
	}
	if err := writeTable(os.Stdout, []string{"REF", "TITLE", "STATUS"}, rows); err != nil {
		return err
	}
	if page.NextCursor != "" {
		_, _ = fmt.Fprintf(os.Stdout, "next_cursor: %s\n", page.NextCursor)
	}
	return nil
}

func runDecisionGet(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("decision get: expected a decision reference as the first argument")
	}
	ref := args[0]
	fs, cfg, err := newClientFlagSet("decision get")
	if err != nil {
		return err
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	d, err := cfg.newClient().GetDecision(context.Background(), ref)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, d)
	}
	enabled := colorEnabled(cfg)
	return writeTable(os.Stdout, []string{"REF", "TITLE", "STATUS", "VERSION"},
		[][]string{{d.Ref, d.Title, colorStatus(d.Status, enabled), fmt.Sprintf("%d", d.Version)}})
}

func runDecisionCreate(args []string) error {
	fs, cfg, err := newClientFlagSet("decision create")
	if err != nil {
		return err
	}
	title := fs.String("title", "", "the decision's title (required)")
	decisionContext := fs.String("context", "", "Markdown: the situation prompting this decision, given inline")
	contextFile := fs.String("context-file", "", "path to a file containing the context, or - for stdin")
	decisionText := fs.String("decision", "", "Markdown: what was decided, given inline")
	decisionFile := fs.String("decision-file", "", "path to a file containing the decision text, or - for stdin")
	rationale := fs.String("rationale", "", "Markdown: why, given inline")
	rationaleFile := fs.String("rationale-file", "", "path to a file containing the rationale, or - for stdin")
	consequences := fs.String("consequences", "", "Markdown: what this decision leads to, given inline")
	consequencesFile := fs.String("consequences-file", "", "path to a file containing the consequences, or - for stdin")
	idempotencyKey := fs.String("idempotency-key", "", "optional: a client-chosen key that makes a retried call safe — reusing the same key with the same content returns the original decision instead of creating a duplicate")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	if cfg.Project == "" {
		return fmt.Errorf("decision create: --project or TICKETS_PROJECT is required")
	}
	set := visitedFlags(fs)
	if !set["title"] {
		return fmt.Errorf("decision create: --title is required")
	}

	fields := []struct {
		name string
		set  bool
	}{
		{"context", set["context"] && set["context-file"]},
		{"decision", set["decision"] && set["decision-file"]},
		{"rationale", set["rationale"] && set["rationale-file"]},
		{"consequences", set["consequences"] && set["consequences-file"]},
	}
	for _, f := range fields {
		if f.set {
			return fmt.Errorf("decision create: --%s and --%s-file are mutually exclusive", f.name, f.name)
		}
	}
	resolvedContext, err := resolveTextFlag(*decisionContext, *contextFile, set["context-file"])
	if err != nil {
		return err
	}
	resolvedDecision, err := resolveTextFlag(*decisionText, *decisionFile, set["decision-file"])
	if err != nil {
		return err
	}
	resolvedRationale, err := resolveTextFlag(*rationale, *rationaleFile, set["rationale-file"])
	if err != nil {
		return err
	}
	resolvedConsequences, err := resolveTextFlag(*consequences, *consequencesFile, set["consequences-file"])
	if err != nil {
		return err
	}

	d, err := cfg.newClient().CreateDecision(context.Background(), cfg.Project, apiclient.CreateDecisionRequest{
		Title: *title, Context: resolvedContext, Decision: resolvedDecision, Rationale: resolvedRationale, Consequences: resolvedConsequences,
	}, *idempotencyKey)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, d)
	}
	_, err = fmt.Fprintf(os.Stdout, "%s created (version %d)\n", d.Ref, d.Version)
	return err
}

func runDecisionUpdate(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("decision update: expected a decision reference as the first argument")
	}
	ref := args[0]
	fs, cfg, err := newClientFlagSet("decision update")
	if err != nil {
		return err
	}
	title := fs.String("title", "", "the decision's new title (required — full-representation update)")
	decisionContext := fs.String("context", "", "the decision's new context, given inline (defaults to the current context if omitted)")
	contextFile := fs.String("context-file", "", "path to a file containing the new context, or - for stdin")
	decisionText := fs.String("decision", "", "the decision's new decision text, given inline (defaults to the current decision text if omitted)")
	decisionFile := fs.String("decision-file", "", "path to a file containing the new decision text, or - for stdin")
	rationale := fs.String("rationale", "", "the decision's new rationale, given inline (defaults to the current rationale if omitted)")
	rationaleFile := fs.String("rationale-file", "", "path to a file containing the new rationale, or - for stdin")
	consequences := fs.String("consequences", "", "the decision's new consequences, given inline (defaults to the current consequences if omitted)")
	consequencesFile := fs.String("consequences-file", "", "path to a file containing the new consequences, or - for stdin")
	status := fs.String("status", "", "proposed, accepted, rejected, or superseded (required)")
	supersededBy := fs.String("superseded-by", "", "reference of the decision that supersedes this one, or \"\" to clear (defaults to the current value if omitted)")
	ifVersion := fs.Int64("if-version", 0, "the decision's current version, from a prior decision get (required)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	set := visitedFlags(fs)
	if !set["if-version"] {
		return fmt.Errorf("decision update: --if-version is required")
	}
	if !set["title"] || !set["status"] {
		return fmt.Errorf("decision update: --title and --status are required (full-representation update)")
	}

	fields := []struct {
		name string
		set  bool
	}{
		{"context", set["context"] && set["context-file"]},
		{"decision", set["decision"] && set["decision-file"]},
		{"rationale", set["rationale"] && set["rationale-file"]},
		{"consequences", set["consequences"] && set["consequences-file"]},
	}
	for _, f := range fields {
		if f.set {
			return fmt.Errorf("decision update: --%s and --%s-file are mutually exclusive", f.name, f.name)
		}
	}

	client := cfg.newClient()
	var current apiclient.Decision
	needCurrent := (!set["context"] && !set["context-file"]) ||
		(!set["decision"] && !set["decision-file"]) ||
		(!set["rationale"] && !set["rationale-file"]) ||
		(!set["consequences"] && !set["consequences-file"]) ||
		!set["superseded-by"]
	if needCurrent {
		// Full-representation update: any field left unset would otherwise
		// be sent as "" and silently wipe the current value server-side.
		current, err = client.GetDecision(context.Background(), ref)
		if err != nil {
			return err
		}
	}

	resolvedContext, err := resolveTextFlagOr(*decisionContext, *contextFile, set["context"], set["context-file"], current.Context)
	if err != nil {
		return err
	}
	resolvedDecision, err := resolveTextFlagOr(*decisionText, *decisionFile, set["decision"], set["decision-file"], current.Decision)
	if err != nil {
		return err
	}
	resolvedRationale, err := resolveTextFlagOr(*rationale, *rationaleFile, set["rationale"], set["rationale-file"], current.Rationale)
	if err != nil {
		return err
	}
	resolvedConsequences, err := resolveTextFlagOr(*consequences, *consequencesFile, set["consequences"], set["consequences-file"], current.Consequences)
	if err != nil {
		return err
	}
	resolvedSupersededBy := *supersededBy
	if !set["superseded-by"] && current.SupersededBy != nil {
		resolvedSupersededBy = *current.SupersededBy
	}

	d, err := client.UpdateDecision(context.Background(), ref, apiclient.UpdateDecisionRequest{
		Title: *title, Context: resolvedContext, Decision: resolvedDecision, Rationale: resolvedRationale, Consequences: resolvedConsequences,
		Status: *status, SupersededBy: resolvedSupersededBy,
	}, *ifVersion)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, d)
	}
	_, err = fmt.Fprintf(os.Stdout, "%s updated (version %d)\n", d.Ref, d.Version)
	return err
}

func runDecisionVersions(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("decision versions: expected a decision reference as the first argument")
	}
	ref := args[0]
	fs, cfg, err := newClientFlagSet("decision versions")
	if err != nil {
		return err
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	page, err := cfg.newClient().ListDecisionVersions(context.Background(), ref)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, page)
	}
	enabled := colorEnabled(cfg)
	rows := make([][]string, len(page.Versions))
	for i, v := range page.Versions {
		rows[i] = []string{fmt.Sprintf("%d", v.Version), v.Title, colorStatus(v.Status, enabled), v.EditedBy, v.CreatedAt.Format("2006-01-02T15:04:05Z")}
	}
	return writeTable(os.Stdout, []string{"VERSION", "TITLE", "STATUS", "EDITED_BY", "CREATED_AT"}, rows)
}

func runDecisionDiff(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("decision diff: expected a decision reference as the first argument")
	}
	ref := args[0]
	fs, cfg, err := newClientFlagSet("decision diff")
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
		return fmt.Errorf("decision diff: --from and --to are required")
	}

	diff, err := cfg.newClient().GetDecisionDiff(context.Background(), ref, *from, *to)
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
		{"context", diff.Context},
		{"decision", diff.Decision},
		{"rationale", diff.Rationale},
		{"consequences", diff.Consequences},
	}
	if diff.StatusFrom != diff.StatusTo {
		_, _ = fmt.Fprintf(os.Stdout, "status: %s -> %s\n", diff.StatusFrom, diff.StatusTo)
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

// resolveTextFlag returns fileFlag's file contents (via readBodyFile)
// when useFile is set, otherwise inline as-is.
func resolveTextFlag(inline, fileFlag string, useFile bool) (string, error) {
	if !useFile {
		return inline, nil
	}
	return readBodyFile(fileFlag)
}

// resolveTextFlagOr is resolveTextFlag extended for full-representation
// updates: when neither the inline nor the file flag was set, it falls
// back to fallback (the field's current server-side value) instead of "",
// so an omitted flag leaves the field unchanged rather than wiping it.
func resolveTextFlagOr(inline, fileFlag string, useInline, useFile bool, fallback string) (string, error) {
	switch {
	case useFile:
		return readBodyFile(fileFlag)
	case useInline:
		return inline, nil
	default:
		return fallback, nil
	}
}
