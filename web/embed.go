// Package web embeds the built React web UI so the Tickets server
// ships as a single executable. dist/ is Vite build output (`task
// web:build` / `npm run build` inside web/) — gitignored except a
// tracked dist/.gitkeep placeholder, so a bare `go build`/`go test`
// checkout that never ran npm still compiles and passes (ADR 0010),
// serving/testing against an effectively-empty embedded filesystem
// instead of a real UI. internal/httpapi's static handler (static.go)
// returns a clear 500 pointing at `task web:build` when index.html is
// missing, rather than a confusing 404.
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
