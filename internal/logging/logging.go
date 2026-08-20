package logging

import (
	"log/slog"
	"os"
)

// New builds a logger writing to stderr in the given format: "json"
// for machine-readable output, anything else (including the empty
// string) for human-readable console output. config.Load already
// rejects any Config.LogFormat other than "console"/"json" before a
// caller gets this far, so New itself doesn't need to error on an
// unrecognized value - it can only mean a caller built a Config by
// hand outside Load, and console output is a reasonable fallback for
// that case.
func New(format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	return slog.New(handler)
}
