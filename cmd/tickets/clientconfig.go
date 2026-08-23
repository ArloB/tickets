package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ArloB/tickets/internal/apiclient"
)

// defaultAPIURL matches cmd/tickets/mcp.go's own default, so a client
// command and `tickets mcp` talk to the same server out of the box
// with no configuration at all.
const defaultAPIURL = "http://127.0.0.1:8080/api/v1"

// clientConfig is a client-mode command's resolved connection info —
// docs/contracts/cli.md's precedence (lowest to highest): built-in
// defaults, the client config file, TICKETS_* environment variables,
// then flags. This is the CLI-as-remote-client counterpart to
// internal/config.Load, which resolves the server's own settings the
// same layered way.
type clientConfig struct {
	APIURL  string
	Token   string
	Project string
	JSON    bool
	NoColor bool

	// tokenStdin is bound to --token-stdin by newClientFlagSet; finish
	// reads stdin only after fs.Parse has run, once every other flag
	// (including --url, needed to know where to send the eventual
	// requests this token authenticates) is already resolved.
	tokenStdin *bool
}

// newClient builds an apiclient.Client from the resolved config.
func (c *clientConfig) newClient() *apiclient.Client {
	return &apiclient.Client{BaseURL: c.APIURL, Token: c.Token}
}

// finish completes resolution after fs.Parse: --token-stdin, if set,
// overrides whatever token the file/env layers resolved (flags are
// the highest-priority layer). Called once per command, after flag
// parsing, since reading stdin before knowing whether the flag was
// actually passed would consume input a command that never asked for
// it shouldn't touch.
func (c *clientConfig) finish() error {
	if c.tokenStdin != nil && *c.tokenStdin {
		token, err := readTokenFromStdin()
		if err != nil {
			return fmt.Errorf("--token-stdin: %w", err)
		}
		c.Token = token
	}
	return nil
}

func readTokenFromStdin() (string, error) {
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("no token read from stdin")
	}
	return strings.TrimSpace(scanner.Text()), nil
}

// clientFileConfig mirrors clientConfig with every field optional —
// same reasoning as internal/config's fileConfig: an absent JSON key
// must leave the corresponding field untouched, not silently override
// a value an earlier/later layer already resolved.
type clientFileConfig struct {
	APIURL  *string `json:"api_url"`
	Token   *string `json:"token"`
	Project *string `json:"project"`
}

// clientConfigFilePath is the client config file's location, unless
// TICKETS_CLIENT_CONFIG_FILE overrides it. Deliberately a different
// file from the server's own config.json (internal/config.configFilePath):
// a CLI operator and a server operator are often different concerns
// even on one machine.
func clientConfigFilePath() string {
	if v := os.Getenv("TICKETS_CLIENT_CONFIG_FILE"); v != "" {
		return v
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		return ""
	}
	return filepath.Join(base, "tickets", "client.json")
}

func loadClientFileConfig(path string) (clientFileConfig, error) {
	if path == "" {
		return clientFileConfig{}, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return clientFileConfig{}, nil
	}
	if err != nil {
		return clientFileConfig{}, fmt.Errorf("read %s: %w", path, err)
	}
	var fc clientFileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return clientFileConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return fc, nil
}

// newClientFlagSet resolves defaults/file/env into cfg and registers
// the flags every client-mode subcommand shares, but does not call
// fs.Parse: the caller registers its own subcommand-specific flags
// (e.g. --limit/--cursor) first, then calls fs.Parse(args) once so
// every flag — shared and subcommand-specific alike — is parsed
// together, followed by cfg.finish() to resolve --token-stdin.
//
// There is deliberately no --token flag here (docs/contracts/cli.md):
// only the config file, TICKETS_API_TOKEN, or --token-stdin can supply
// one, so a token never lands in a human's shell history the way a
// flag value would. Compare cmd/tickets/mcp.go's `--token`, which is
// fine specifically because it's driven by a non-interactive
// .mcp.json, not typed at a shell.
func newClientFlagSet(name string) (*flag.FlagSet, *clientConfig, error) {
	fc, err := loadClientFileConfig(clientConfigFilePath())
	if err != nil {
		return nil, nil, err
	}

	cfg := &clientConfig{APIURL: defaultAPIURL}
	if fc.APIURL != nil {
		cfg.APIURL = *fc.APIURL
	}
	if fc.Token != nil {
		cfg.Token = *fc.Token
	}
	if fc.Project != nil {
		cfg.Project = *fc.Project
	}

	cfg.APIURL = envOr("TICKETS_API_URL", cfg.APIURL)
	cfg.Token = envOr("TICKETS_API_TOKEN", cfg.Token)
	cfg.Project = envOr("TICKETS_PROJECT", cfg.Project)

	// NO_COLOR (https://no-color.org: any non-empty value disables
	// color) resolves before --no-color registers, same precedence
	// every other setting here follows — the flag can still override it
	// either way on the command line.
	cfg.NoColor = envOr("NO_COLOR", "") != ""

	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.StringVar(&cfg.APIURL, "url", cfg.APIURL, "base URL of the Tickets HTTP API")
	fs.StringVar(&cfg.Project, "project", cfg.Project, "default project key filled in when a command omits one")
	fs.BoolVar(&cfg.JSON, "json", false, "output stable JSON instead of a human-readable table")
	fs.BoolVar(&cfg.NoColor, "no-color", cfg.NoColor, "disable ANSI color in table output (also set by NO_COLOR)")
	cfg.tokenStdin = fs.Bool("token-stdin", false, "read the bearer token from one line of stdin, instead of the config file or TICKETS_API_TOKEN")

	return fs, cfg, nil
}
