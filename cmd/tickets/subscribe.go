package main

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// runSubscribe/runUnsubscribe are `tickets subscribe <ref>` /
// `tickets unsubscribe <ref>` — flat top-level commands, not a
// subcommand of ticket/feature/decision/plan/document, since every
// principal kind is subscribable (product spec §6.4) and ref itself
// already carries which kind it names; the same reasoning `search`
// (search.go) is a flat command for.
func runSubscribe(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("subscribe: expected a reference as the first argument, e.g. `tickets subscribe ABC-123`")
	}
	ref := args[0]
	fs, cfg, err := newClientFlagSet("subscribe")
	if err != nil {
		return err
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	sub, err := cfg.newClient().Subscribe(context.Background(), ref)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, sub)
	}
	_, _ = fmt.Fprintf(os.Stdout, "subscribed to %s\n", ref)
	return nil
}

func runUnsubscribe(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("unsubscribe: expected a reference as the first argument, e.g. `tickets unsubscribe ABC-123`")
	}
	ref := args[0]
	fs, cfg, err := newClientFlagSet("unsubscribe")
	if err != nil {
		return err
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	sub, err := cfg.newClient().Unsubscribe(context.Background(), ref)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, sub)
	}
	_, _ = fmt.Fprintf(os.Stdout, "unsubscribed from %s\n", ref)
	return nil
}
