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

	var err error
	switch os.Args[1] {
	case "server":
		err = runServer(os.Args[2:])
	case "setup":
		err = runSetup(os.Args[2:])
	case "mcp":
		err = runMCPBridge(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "tickets: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "tickets %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: tickets <command> [flags]

commands:
  server   run the Tickets HTTP server (API and MCP Streamable HTTP)
  setup    first-run administrative setup
  mcp      run the MCP stdio bridge against a configured Tickets server`)
}
