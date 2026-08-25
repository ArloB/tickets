package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ArloB/tickets/internal/apiclient"
)

// runLink is `tickets link <subcommand>` (product spec §5.11's named
// external links — carry-in #10 from the Phase 6 plan: this surface
// existed over HTTP and in the web UI since Phase 4 but had no CLI
// command). ref may be a ticket, feature, decision, plan, or document
// reference — the same five kinds internal/httpapi/server.go registers
// addLink/listLinks/removeLink under.
func runLink(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("link: expected a subcommand (add, list, remove)")
	}
	switch args[0] {
	case "add":
		return runLinkAdd(args[1:])
	case "list":
		return runLinkList(args[1:])
	case "remove":
		return runLinkRemove(args[1:])
	default:
		return fmt.Errorf("link: unknown subcommand %q", args[0])
	}
}

func runLinkAdd(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("link add: expected a reference (ticket, feature, decision, plan, or document) as the first argument")
	}
	ref := args[0]
	fs, cfg, err := newClientFlagSet("link add")
	if err != nil {
		return err
	}
	title := fs.String("title", "", "the link's display title (required)")
	linkURL := fs.String("link-url", "", "the link target, an http(s) or mailto URL (required) — not --url, which is the Tickets server's own address")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	set := visitedFlags(fs)
	if !set["title"] {
		return fmt.Errorf("link add: --title is required")
	}
	if !set["link-url"] {
		return fmt.Errorf("link add: --link-url is required")
	}

	link, err := cfg.newClient().AddLink(context.Background(), ref, apiclient.AddLinkRequest{Title: *title, URL: *linkURL})
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, link)
	}
	_, err = fmt.Fprintf(os.Stdout, "link %d added to %s: %s (%s)\n", link.ID, ref, link.Title, link.URL)
	return err
}

func runLinkList(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("link list: expected a reference (ticket, feature, decision, plan, or document) as the first argument")
	}
	ref := args[0]
	fs, cfg, err := newClientFlagSet("link list")
	if err != nil {
		return err
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	links, err := cfg.newClient().ListLinks(context.Background(), ref)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, links)
	}
	rows := make([][]string, len(links))
	for i, l := range links {
		rows[i] = []string{strconv.FormatInt(l.ID, 10), l.Title, l.URL}
	}
	return writeTable(os.Stdout, []string{"ID", "TITLE", "URL"}, rows)
}

func runLinkRemove(args []string) error {
	if len(args) < 2 || strings.HasPrefix(args[0], "-") || strings.HasPrefix(args[1], "-") {
		return fmt.Errorf("link remove: expected a reference and a link id as the first two arguments")
	}
	ref := args[0]
	id, perr := strconv.ParseInt(args[1], 10, 64)
	if perr != nil {
		return fmt.Errorf("link remove: link id must be an integer, got %q", args[1])
	}
	fs, cfg, err := newClientFlagSet("link remove")
	if err != nil {
		return err
	}
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	if err := cfg.newClient().RemoveLink(context.Background(), ref, id); err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, map[string]string{"status": "removed"})
	}
	_, err = fmt.Fprintf(os.Stdout, "link %d removed from %s\n", id, ref)
	return err
}
