// Package config resolves server and CLI configuration from, in
// increasing priority: built-in defaults, an OS-appropriate config
// file (path overridable via TICKETS_CONFIG_FILE), TICKETS_*
// environment variables, and command-line flags (product spec §7.3).
//
// Phase 0 implemented only --data-dir and the loopback-by-default bind
// address. Phase 2 added the full precedence chain plus the remaining
// server settings (anonymous-read, log format, shutdown timeout).
package config
