package httpapi

import (
	"net/http"
	"testing"
)

// TestEveryMutatingRouteRequiresAtLeastEditor reads server.go's
// routeTable directly (no HTTP round trip) and asserts every non-GET
// route requires at least Editor permission. This is the regression
// test server.go's routeTable doc comment promises: without it, a
// route added with routeViewer by mistake — the exact failure shape
// that already bit this codebase once with domain.ErrForbidden's
// missing statusForCode entry — would let an anonymous viewer mutate
// data, and nothing but a manual review would catch it. Because
// NewHandler wraps purely as a function of routeTable's permission
// field, this table is the single source of truth for what "wrapped"
// means; there is no separate runtime behavior to reconcile against.
func TestEveryMutatingRouteRequiresAtLeastEditor(t *testing.T) {
	s := &Server{}
	for _, e := range s.routeTable() {
		if e.method == http.MethodGet {
			continue
		}
		if e.permission == routeViewer {
			t.Errorf("route %s %s is registered with routeViewer permission — every non-GET route must require at least routeEditor, or an anonymous viewer (or any authenticated non-editor) could mutate data", e.method, e.pattern)
		}
	}
}
