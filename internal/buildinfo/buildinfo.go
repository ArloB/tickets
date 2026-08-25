// Package buildinfo carries the running binary's version, commit, and
// build date — product spec §12's backup manifest and §13's
// observability both need a way to record which server version
// produced a given artifact or log line, and product spec §16's
// acceptance criteria don't tie any of this to a schema migration, so
// it stays a separate, much lighter-weight identifier than
// internal/store's schema_migrations version.
//
// Version/Commit/Date are set via -ldflags from `task build`/`task
// release` (Phase 6 Step 10). A bare `go build ./cmd/tickets` — the
// "Go-only contributor" guarantee ADR 0010 makes for the embedded web
// assets — still compiles and reports sane defaults, never a build
// failure or an empty string, since nothing here requires the linker
// flags to be present.
package buildinfo

var (
	// Version is a released tag (e.g. "v0.6.0") or, for an
	// unreleased/local build, "dev".
	Version = "dev"
	// Commit is the short Git commit hash the binary was built from,
	// or "none" when unavailable (no .git directory, a source
	// tarball, etc).
	Commit = "none"
	// Date is the build's UTC timestamp in RFC 3339, or "unknown" for
	// a bare `go build`.
	Date = "unknown"
)

// String renders a single human-readable line — `tickets --version`'s
// output and the value logged once at server startup.
func String() string {
	return "tickets " + Version + " (commit " + Commit + ", built " + Date + ")"
}
