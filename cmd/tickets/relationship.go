package main

import (
	"context"
	"fmt"
	"os"
)

func runTicketRelate(args []string) error {
	ref, rest, err := popTicketRef("ticket relate", args)
	if err != nil {
		return err
	}
	fs, cfg, err := newClientFlagSet("ticket relate")
	if err != nil {
		return err
	}
	relType := fs.String("type", "", "one of: parent_of, child_of, blocks, blocked_by, related_to, duplicate_of, supersedes, superseded_by (required)")
	target := fs.String("target", "", "the other ticket's reference, e.g. ABC-124 (required)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	set := visitedFlags(fs)
	if !set["type"] || !set["target"] {
		return fmt.Errorf("ticket relate: --type and --target are both required")
	}

	if err := cfg.newClient().AddRelationship(context.Background(), ref, *relType, *target); err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, map[string]string{"ref": ref, "type": *relType, "target": *target})
	}
	_, err = fmt.Fprintf(os.Stdout, "%s %s %s\n", ref, *relType, *target)
	return err
}

func runTicketRelationships(args []string) error {
	ref, rest, err := popTicketRef("ticket relationships", args)
	if err != nil {
		return err
	}
	fs, cfg, err := newClientFlagSet("ticket relationships")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	page, err := cfg.newClient().ListRelationships(context.Background(), ref)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, page)
	}
	rows := make([][]string, len(page.Relationships))
	for i, r := range page.Relationships {
		rows[i] = []string{r.Type, r.Other}
	}
	return writeTable(os.Stdout, []string{"TYPE", "OTHER"}, rows)
}

func runTicketUnrelate(args []string) error {
	ref, rest, err := popTicketRef("ticket unrelate", args)
	if err != nil {
		return err
	}
	fs, cfg, err := newClientFlagSet("ticket unrelate")
	if err != nil {
		return err
	}
	relType := fs.String("type", "", "the relationship type to remove (required)")
	target := fs.String("target", "", "the other ticket's reference (required)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	set := visitedFlags(fs)
	if !set["type"] || !set["target"] {
		return fmt.Errorf("ticket unrelate: --type and --target are both required")
	}

	if err := cfg.newClient().RemoveRelationship(context.Background(), ref, *relType, *target); err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, map[string]string{"status": "removed"})
	}
	_, err = fmt.Fprintf(os.Stdout, "%s %s %s removed\n", ref, *relType, *target)
	return err
}
