package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
)

// TestColdStartFreshDataDirWithUnusualPath is Phase 6 Step 8's
// platform-testing drill for §16 criterion 1 ("a single executable
// starts a fresh server... on Linux and Windows") and §15's explicit
// "data directory paths containing spaces and non-ASCII characters"
// requirement, combined into one scenario neither
// internal/store.TestOpenUnusualPath (store-only) nor
// TestRootHandlerComposition (ordinary temp dir) covers: a completely
// fresh data directory, path containing both a space and a non-ASCII
// character, taken through the same store.Open -> blobstore.Open ->
// newRootHandler sequence runServer uses, with /healthz confirmed
// reachable and the whole cold start timed against §11's "should
// normally complete within two seconds" target.
//
// This runs on whatever OS the test binary executes on — on this
// repo's own CI that's Linux (WSL) today; Windows coverage of this
// same scenario still requires an actual native-Windows run (`task
// ci`/`task build` from a Windows shell, not WSL), which is outside
// what a Go test executing here can exercise. See docs/mvp-acceptance.md
// row 1's notes.
func TestColdStartFreshDataDirWithUnusualPath(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "tickets dätá dir")

	start := time.Now()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open(%q): %v", dataDir, err)
	}
	defer func() { _ = st.Close() }()
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatalf("blobstore.Open(%q): %v", dataDir, err)
	}
	svc := service.New(st, blobs)
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Errorf("cold start (store.Open + blobstore.Open) took %v, want under 2s per §11's startup target", elapsed)
	}

	ts := httptest.NewServer(newRootHandler(svc, true))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /healthz status = %d, want 200", resp.StatusCode)
	}

	if err := svc.Ping(context.Background()); err != nil {
		t.Errorf("Ping over the unusual-path store: %v", err)
	}
}

// TestUpgradeOverExistingDataDirWithUnusualPath is
// TestColdStartFreshDataDirWithUnusualPath's counterpart for §16
// criterion 1's other half — "upgrade" (reopening an existing data
// directory, not just a fresh one) — combined with the same unusual-
// path requirement. Opens, closes, and reopens the same data
// directory, confirming the second open (the "upgrade" a real restart
// performs) succeeds and sees the state the first open wrote, still
// under the unusual path.
func TestUpgradeOverExistingDataDirWithUnusualPath(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "tickets dätá dir")

	st1, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("first store.Open(%q): %v", dataDir, err)
	}
	blobs1, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatalf("first blobstore.Open(%q): %v", dataDir, err)
	}
	svc1 := service.New(st1, blobs1)
	ctx := context.Background()
	actor := domain.ActorRef{Kind: domain.ActorHuman, Name: "local"}
	if _, err := svc1.CreateProject(ctx, service.CreateProjectRequest{Key: "ABC", Title: "Example"}, actor, "cid-1", "", ""); err != nil {
		t.Fatalf("create project before reopen: %v", err)
	}
	if err := st1.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	start := time.Now()
	st2, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("reopen (upgrade path) store.Open(%q): %v", dataDir, err)
	}
	defer func() { _ = st2.Close() }()
	blobs2, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatalf("reopen blobstore.Open(%q): %v", dataDir, err)
	}
	svc2 := service.New(st2, blobs2)
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Errorf("reopen (upgrade path) took %v, want under 2s per §11's startup target", elapsed)
	}

	proj, err := svc2.GetProject(ctx, "ABC")
	if err != nil {
		t.Fatalf("get project after reopen: %v", err)
	}
	if proj.Title != "Example" {
		t.Errorf("project title after reopen = %q, want %q", proj.Title, "Example")
	}
}
