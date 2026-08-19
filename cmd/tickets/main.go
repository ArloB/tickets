// Command tickets is the single entry point for the Tickets server, CLI,
// and MCP bridge. Subcommands dispatch into internal packages; no
// application logic lives in this package.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "server":
		// Implemented in Phase 0 Step 5 (the vertical-slice PoC): wires
		// internal/config, internal/store, internal/service, and
		// internal/httpapi behind a 127.0.0.1-by-default listener.
		fmt.Fprintln(os.Stderr, "tickets server: not implemented yet")
		os.Exit(1)
	case "setup":
		fmt.Fprintln(os.Stderr, "tickets setup: not implemented yet")
		os.Exit(1)
	case "mcp":
		fmt.Fprintln(os.Stderr, "tickets mcp: not implemented yet")
		os.Exit(1)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "tickets: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: tickets <command> [flags]

commands:
  server   run the Tickets HTTP server (API, web UI, MCP Streamable HTTP)
  setup    first-run administrative setup
  mcp      run the MCP stdio bridge against a configured Tickets server`)
}
