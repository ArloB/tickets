package service

import (
	"context"
	"strings"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

func TestCreateContentItemFileRepresentation(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	doc, err := s.CreateContentItem(ctx, CreateContentItemRequest{
		ProjectKey: "ABC", Kind: domain.KindDocument, Title: "Spec PDF",
		Representation: domain.ContentRepresentationFile,
		Content:        strings.NewReader("pdf bytes"), FileName: "spec.pdf", MediaType: "application/pdf",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem(file): %v", err)
	}
	if doc.Representation != "file" {
		t.Errorf("Representation = %q, want file", doc.Representation)
	}
	if doc.FileName != "spec.pdf" {
		t.Errorf("FileName = %q, want spec.pdf", doc.FileName)
	}
	if doc.Body != "" {
		t.Errorf("Body = %q, want empty for a file representation", doc.Body)
	}

	ref, err := domain.Parse(doc.Ref)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	dl, err := s.DownloadContentItem(ctx, ref)
	if err != nil {
		t.Fatalf("DownloadContentItem: %v", err)
	}
	defer func() { _ = dl.Content.Close() }()
	got := make([]byte, 32)
	n, _ := dl.Content.Read(got)
	if string(got[:n]) != "pdf bytes" {
		t.Errorf("downloaded content = %q, want %q", got[:n], "pdf bytes")
	}
}

func TestCreateContentItemPathRepresentation(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	plan, err := s.CreateContentItem(ctx, CreateContentItemRequest{
		ProjectKey: "ABC", Kind: domain.KindPlan, Title: "External plan",
		Representation: domain.ContentRepresentationPath, PathValue: "/srv/docs/plan.md",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem(path): %v", err)
	}
	if plan.PathValue != "/srv/docs/plan.md" {
		t.Errorf("PathValue = %q, want /srv/docs/plan.md", plan.PathValue)
	}

	ref, _ := domain.Parse(plan.Ref)
	if _, err := s.DownloadContentItem(ctx, ref); err == nil {
		t.Error("DownloadContentItem on a path representation: want error, got nil (ADR 0007: never serve a path target)")
	}
}

func TestCreateContentItemURLRepresentation(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	doc, err := s.CreateContentItem(ctx, CreateContentItemRequest{
		ProjectKey: "ABC", Kind: domain.KindDocument, Title: "External wiki page",
		Representation: domain.ContentRepresentationURL, URLValue: "https://wiki.example.com/page",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem(url): %v", err)
	}
	if doc.URLValue != "https://wiki.example.com/page" {
		t.Errorf("URLValue = %q, want https://wiki.example.com/page", doc.URLValue)
	}
}

func TestCreateContentItemURLRepresentationRejectsInvalidScheme(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	if _, err := s.CreateContentItem(ctx, CreateContentItemRequest{
		ProjectKey: "ABC", Kind: domain.KindDocument, Title: "Bad URL",
		Representation: domain.ContentRepresentationURL, URLValue: "javascript:alert(1)",
	}, testActor, testCorrelationID, "", ""); !isValidationError(err, "url") {
		t.Errorf("CreateContentItem with javascript: URL: err = %v, want a validation error on \"url\"", err)
	}
}

func TestCreateContentItemFileRequiresContent(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	if _, err := s.CreateContentItem(ctx, CreateContentItemRequest{
		ProjectKey: "ABC", Kind: domain.KindDocument, Title: "No file",
		Representation: domain.ContentRepresentationFile,
	}, testActor, testCorrelationID, "", ""); !isValidationError(err, "file") {
		t.Errorf("CreateContentItem(file) with no content: err = %v, want a validation error on \"file\"", err)
	}
}

// TestUpdateContentItemPathRepresentationChangesValue confirms an
// update on a path-representation item can change the path value
// (correcting it, or re-pointing it) without being able to switch
// representation — the request struct has no representation field at
// all, so this is enforced structurally, not just by a runtime check.
func TestUpdateContentItemPathRepresentationChangesValue(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	plan, err := s.CreateContentItem(ctx, CreateContentItemRequest{
		ProjectKey: "ABC", Kind: domain.KindPlan, Title: "Plan",
		Representation: domain.ContentRepresentationPath, PathValue: "/old/path.md",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem: %v", err)
	}
	ref, _ := domain.Parse(plan.Ref)

	updated, err := s.UpdateContentItem(ctx, UpdateContentItemRequest{
		Ref: ref, Title: "Plan", PathValue: "/new/path.md", ExpectedVersion: plan.Version,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("UpdateContentItem: %v", err)
	}
	if updated.PathValue != "/new/path.md" {
		t.Errorf("PathValue after update = %q, want /new/path.md", updated.PathValue)
	}
	if updated.Representation != "path" {
		t.Errorf("Representation after update = %q, want path (immutable)", updated.Representation)
	}

	versions, err := s.ListContentItemVersions(ctx, ref)
	if err != nil {
		t.Fatalf("ListContentItemVersions: %v", err)
	}
	if len(versions) != 1 || versions[0].PathValue != "/old/path.md" {
		t.Fatalf("archived versions = %+v, want exactly one entry with the old path value", versions)
	}
}

func TestUpdateContentItemFileRepresentationNewVersion(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateProject(t, s, "ABC")

	doc, err := s.CreateContentItem(ctx, CreateContentItemRequest{
		ProjectKey: "ABC", Kind: domain.KindDocument, Title: "Spec",
		Representation: domain.ContentRepresentationFile,
		Content:        strings.NewReader("v1 bytes"), FileName: "spec-v1.pdf",
	}, testActor, testCorrelationID, "", "")
	if err != nil {
		t.Fatalf("CreateContentItem: %v", err)
	}
	ref, _ := domain.Parse(doc.Ref)

	updated, err := s.UpdateContentItem(ctx, UpdateContentItemRequest{
		Ref: ref, Title: "Spec", Content: strings.NewReader("v2 bytes"), FileName: "spec-v2.pdf",
		ExpectedVersion: doc.Version,
	}, testActor, testCorrelationID)
	if err != nil {
		t.Fatalf("UpdateContentItem: %v", err)
	}
	if updated.FileName != "spec-v2.pdf" {
		t.Errorf("FileName after update = %q, want spec-v2.pdf", updated.FileName)
	}

	dl, err := s.DownloadContentItem(ctx, ref)
	if err != nil {
		t.Fatalf("DownloadContentItem: %v", err)
	}
	defer func() { _ = dl.Content.Close() }()
	got := make([]byte, 32)
	n, _ := dl.Content.Read(got)
	if string(got[:n]) != "v2 bytes" {
		t.Errorf("downloaded content after update = %q, want %q", got[:n], "v2 bytes")
	}

	versionDl, err := s.DownloadContentItemVersion(ctx, ref, 1)
	if err != nil {
		t.Fatalf("DownloadContentItemVersion(1): %v", err)
	}
	defer func() { _ = versionDl.Content.Close() }()
	got2 := make([]byte, 32)
	n2, _ := versionDl.Content.Read(got2)
	if string(got2[:n2]) != "v1 bytes" {
		t.Errorf("downloaded version 1 content = %q, want %q", got2[:n2], "v1 bytes")
	}
}
