// Command tickets is the single entry point for the Tickets server, CLI,
// and MCP bridge. Subcommands dispatch into internal packages; no
// application logic lives in this package.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/ArloB/tickets/internal/apiclient"
	"github.com/ArloB/tickets/internal/service"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "server":
		err = runServer(os.Args[2:])
	case "setup":
		err = runSetup(os.Args[2:])
	case "mcp":
		err = runMCPBridge(os.Args[2:])
	case "admin":
		err = runAdmin(os.Args[2:])
	case "project":
		err = runProject(os.Args[2:])
	case "feature":
		err = runFeature(os.Args[2:])
	case "ticket":
		err = runTicket(os.Args[2:])
	case "comment":
		err = runComment(os.Args[2:])
	case "decision":
		err = runDecision(os.Args[2:])
	case "plan":
		err = runPlan(os.Args[2:])
	case "document":
		err = runDocument(os.Args[2:])
	case "activity":
		err = runActivity(os.Args[2:])
	case "attachment":
		err = runAttachment(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "tickets: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}

	if err != nil {
		var cerr *apiclient.Error
		if errors.As(err, &cerr) {
			// "<code>: <message>" — the same convention
			// internal/mcpsrv's toolError uses for MCP tool errors
			// (docs/contracts/errors.md's shared vocabulary), so a
			// script can match on the leading code token without
			// needing --json.
			fmt.Fprintf(os.Stderr, "tickets %s: %s: %s\n", os.Args[1], cerr.Code, cerr.Message)
			os.Exit(exitCode(cerr.Code))
		}
		// admin agent/token commands (admin_agent.go) call
		// *service.Service directly rather than going through
		// apiclient, so their errors surface as *service.Error
		// instead — same code/message shape, same exit table.
		var serr *service.Error
		if errors.As(err, &serr) {
			fmt.Fprintf(os.Stderr, "tickets %s: %s: %s\n", os.Args[1], serr.Code, serr.Message)
			os.Exit(exitCode(serr.Code))
		}
		fmt.Fprintf(os.Stderr, "tickets %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: tickets <command> [flags]

commands:
  server   run the Tickets HTTP server (API and MCP Streamable HTTP)
  setup    first-run administrative setup
  mcp      run the MCP stdio bridge against a configured Tickets server
  admin    maintenance operations (purge-idempotency-keys, agent, token)
  project  client commands against a running Tickets server (list, create)
  feature  client commands against a running Tickets server (list, get, create, update)
  ticket   client commands against a running Tickets server (list, get, update, assign, move, delete, restore, relate, relationships, unrelate, associate, associations, disassociate)
  comment  client commands against a running Tickets server (add, list, edit, delete)
  decision client commands against a running Tickets server (list, get, create, update, versions, diff)
  plan     client commands against a running Tickets server (list, get, create, update, versions, diff, download)
  document client commands against a running Tickets server (list, get, create, update, versions, diff, download)
  activity client commands against a running Tickets server (list)
  attachment client commands against a running Tickets server (upload, path, list, get, versions, download, replace, delete)`)
}
