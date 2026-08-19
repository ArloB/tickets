// Package web embeds the built React web UI so the Tickets server ships
// as a single executable. Until Phase 4, dist/ holds only a placeholder
// page committed so go:embed and go build succeed.
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
