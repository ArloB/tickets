package main

import (
	"context"
	"fmt"
	"os"
)

func runTicketAssociate(args []string) error {
	ref, rest, err := popTicketRef("ticket associate", args)
	if err != nil {
		return err
	}
	fs, cfg, err := newClientFlagSet("ticket associate")
	if err != nil {
		return err
	}
	target := fs.String("target", "", "the other entity's reference, e.g. ABC-124 or ABC-F2 (required)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	if !visitedFlags(fs)["target"] {
		return fmt.Errorf("ticket associate: --target is required")
	}

	if err := cfg.newClient().AddAssociation(context.Background(), ref, *target); err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, map[string]string{"ref": ref, "target": *target})
	}
	_, err = fmt.Fprintf(os.Stdout, "%s associated with %s\n", ref, *target)
	return err
}

func runTicketAssociations(args []string) error {
	ref, rest, err := popTicketRef("ticket associations", args)
	if err != nil {
		return err
	}
	fs, cfg, err := newClientFlagSet("ticket associations")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	page, err := cfg.newClient().ListAssociations(context.Background(), ref)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, page)
	}
	rows := make([][]string, len(page.Associated))
	for i, a := range page.Associated {
		rows[i] = []string{a}
	}
	return writeTable(os.Stdout, []string{"ASSOCIATED"}, rows)
}

func runTicketDisassociate(args []string) error {
	ref, rest, err := popTicketRef("ticket disassociate", args)
	if err != nil {
		return err
	}
	fs, cfg, err := newClientFlagSet("ticket disassociate")
	if err != nil {
		return err
	}
	target := fs.String("target", "", "the other entity's reference to remove the association with (required)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	if !visitedFlags(fs)["target"] {
		return fmt.Errorf("ticket disassociate: --target is required")
	}

	if err := cfg.newClient().RemoveAssociation(context.Background(), ref, *target); err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, map[string]string{"status": "removed"})
	}
	_, err = fmt.Fprintf(os.Stdout, "%s disassociated from %s\n", ref, *target)
	return err
}
