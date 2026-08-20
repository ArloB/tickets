// Package logging builds the process-wide structured logger (product
// spec §13): human-readable console output by default, JSON when
// internal/config.Config.LogFormat is "json". Every server component
// should log through the *slog.Logger New returns, or a child derived
// from it via .With, rather than the standard log package, so
// severity, correlation IDs, and other structured fields survive to
// whichever format the operator chose.
package logging
