package httpapi

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/getkin/kin-openapi/openapi3"
)

// TestToTicketDetailExcludesUnwiredFields is wire.go's actual
// regression guard: today, nothing sets domain.Ticket.Assignee or
// .DeletedAt on any HTTP response path, so the OpenAPI contract test
// (server_test.go's live schema validation) can't distinguish "the
// DTO excludes this field" from "nothing happens to set it yet." This
// test sets both directly on a domain.Ticket and asserts the mapped
// DTO's JSON has neither key, proving toTicketDetail does the
// excluding — not the absence of a caller that would populate them.
func TestToTicketDetailExcludesUnwiredFields(t *testing.T) {
	deletedAt := time.Now()
	ticket := domain.Ticket{
		Ref: "ABC-1", ProjectKey: "ABC", FeatureRef: "ABC-F1",
		Type: domain.TicketTypeTask, Title: "T", Status: domain.WorkflowStatusBacklog,
		Priority:  domain.PriorityMedium,
		Assignee:  &domain.ActorRef{Kind: domain.ActorHuman, Name: "local"},
		DeletedAt: &deletedAt,
	}

	b, err := json.Marshal(toTicketDetail(ticket))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := fields["assignee"]; ok {
		t.Errorf("ticketDetail JSON has an \"assignee\" key: %s", b)
	}
	if _, ok := fields["deleted_at"]; ok {
		t.Errorf("ticketDetail JSON has a \"deleted_at\" key: %s", b)
	}
}

// TestNoSchemaExposesABareIntegerID is ADR 0002's assigned Phase 1
// test: no api/openapi.yaml component schema may declare an "id"
// property typed as a bare integer — internal surrogate keys
// (entities.id and friends) must never reach the wire; only a
// formatted reference, a project key, or a UUID may.
//
// This does not (and cannot) cover domain.Comment.ID, which is a
// deliberate exception documented on that type: a comment has no
// ref/uuid to hide the surrogate behind, so its id is the real public
// identity, not a leaked internal detail. Comments have no response
// schema and no endpoint in Phase 1, so this test never sees that
// field either way — a pass here is not evidence that exception was
// re-checked, only that nothing wired up yet violates the general rule.
func TestNoSchemaExposesABareIntegerID(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("load openapi.yaml: %v", err)
	}

	for name, schemaRef := range doc.Components.Schemas {
		schema := schemaRef.Value
		if schema == nil || schema.Properties == nil {
			continue
		}
		idProp, ok := schema.Properties["id"]
		if !ok || idProp.Value == nil {
			continue
		}
		if idProp.Value.Type != nil && idProp.Value.Type.Is("integer") {
			t.Errorf("schema %q declares a bare integer \"id\" property — ADR 0002 forbids exposing an internal surrogate key on the wire", name)
		}
	}
}
