package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// runComment is `tickets comment <subcommand>`. Comments are
// ticket-only in Phase 3 (see internal/service/comment.go's
// AddCommentRequest doc comment) — every subcommand here takes a
// ticket reference or a comment id, never a feature/decision one.
func runComment(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("comment: expected a subcommand (add, list, edit, delete)")
	}
	switch args[0] {
	case "add":
		return runCommentAdd(args[1:])
	case "list":
		return runCommentList(args[1:])
	case "edit":
		return runCommentEdit(args[1:])
	case "delete":
		return runCommentDelete(args[1:])
	default:
		return fmt.Errorf("comment: unknown subcommand %q", args[0])
	}
}

func runCommentAdd(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("comment add: expected a ticket reference as the first argument")
	}
	ref := args[0]
	fs, cfg, err := newClientFlagSet("comment add")
	if err != nil {
		return err
	}
	body := fs.String("body", "", "the comment's Markdown body, given inline")
	bodyFile := fs.String("body-file", "", "path to a file containing the comment's Markdown body, or - for stdin")
	idempotencyKey := fs.String("idempotency-key", "", "optional: a client-chosen key that makes a retried call safe — reusing the same key with the same ref/body returns the original comment instead of creating a duplicate")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	set := visitedFlags(fs)
	if set["body"] == set["body-file"] {
		return fmt.Errorf("comment add: exactly one of --body or --body-file is required")
	}

	text := *body
	if set["body-file"] {
		text, err = readBodyFile(*bodyFile)
		if err != nil {
			return err
		}
	}

	comment, err := cfg.newClient().CreateComment(context.Background(), ref, text, *idempotencyKey)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, comment)
	}
	_, err = fmt.Fprintf(os.Stdout, "comment %d added to %s (version %d)\n", comment.ID, ref, comment.Version)
	return err
}

func runCommentList(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("comment list: expected a ticket reference as the first argument")
	}
	ref := args[0]
	fs, cfg, err := newClientFlagSet("comment list")
	if err != nil {
		return err
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	page, err := cfg.newClient().ListComments(context.Background(), ref)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, page)
	}
	rows := make([][]string, len(page.Comments))
	for i, c := range page.Comments {
		status := "-"
		if c.DeletedAt != nil {
			status = "deleted"
		}
		rows[i] = []string{strconv.FormatInt(c.ID, 10), c.Author, status, strconv.FormatInt(c.Version, 10), c.Body}
	}
	return writeTable(os.Stdout, []string{"ID", "AUTHOR", "STATUS", "VERSION", "BODY"}, rows)
}

// popCommentID extracts a subcommand's leading positional comment id.
func popCommentID(cmd string, args []string) (id int64, rest []string, err error) {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return 0, nil, fmt.Errorf("%s: expected a comment id as the first argument", cmd)
	}
	id, err = strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return 0, nil, fmt.Errorf("%s: comment id must be an integer: %w", cmd, err)
	}
	return id, args[1:], nil
}

func runCommentEdit(args []string) error {
	id, rest, err := popCommentID("comment edit", args)
	if err != nil {
		return err
	}
	fs, cfg, err := newClientFlagSet("comment edit")
	if err != nil {
		return err
	}
	body := fs.String("body", "", "the comment's new Markdown body, given inline")
	bodyFile := fs.String("body-file", "", "path to a file containing the comment's new Markdown body, or - for stdin")
	ifVersion := fs.Int64("if-version", 0, "the comment's current version, from a prior comment list (required)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	set := visitedFlags(fs)
	if !set["if-version"] {
		return fmt.Errorf("comment edit: --if-version is required")
	}
	if set["body"] == set["body-file"] {
		return fmt.Errorf("comment edit: exactly one of --body or --body-file is required")
	}

	text := *body
	if set["body-file"] {
		text, err = readBodyFile(*bodyFile)
		if err != nil {
			return err
		}
	}

	comment, err := cfg.newClient().EditComment(context.Background(), id, *ifVersion, text)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, comment)
	}
	_, err = fmt.Fprintf(os.Stdout, "comment %d updated (version %d)\n", comment.ID, comment.Version)
	return err
}

func runCommentDelete(args []string) error {
	id, rest, err := popCommentID("comment delete", args)
	if err != nil {
		return err
	}
	fs, cfg, err := newClientFlagSet("comment delete")
	if err != nil {
		return err
	}
	ifVersion := fs.Int64("if-version", 0, "the comment's current version, from a prior comment list (required)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	if !visitedFlags(fs)["if-version"] {
		return fmt.Errorf("comment delete: --if-version is required")
	}

	if err := cfg.newClient().DeleteComment(context.Background(), id, *ifVersion); err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, map[string]any{"id": id, "status": "deleted"})
	}
	_, err = fmt.Fprintf(os.Stdout, "comment %d deleted\n", id)
	return err
}
