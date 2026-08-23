package httpapi

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/getkin/kin-openapi/openapi3"
)

// TestToTicketDetailFieldExposure is wire.go's actual regression
// guard for which domain.Ticket fields toTicketDetail does and does
// not carry to the wire. Assignee and Creator are deliberately
// exposed as of Step 13 (see ticketDetail's doc comment); DeletedAt is
// deliberately still excluded, and always will be — no route that
// returns a ticketDetail can ever populate it, since a soft-deleted
// ticket is invisible to every normal read path (ADR 0013), so there
// is no live caller this test could otherwise rely on to prove the
// exclusion is deliberate rather than untested. Setting all three
// directly on a domain.Ticket and inspecting the mapped DTO's JSON
// keys is what makes the "excluded on purpose" half of this test
// possible at all.
func TestToTicketDetailFieldExposure(t *testing.T) {
	deletedAt := time.Now()
	assignee := domain.ActorRef{Kind: domain.ActorHuman, Name: "local"}
	creator := domain.ActorRef{Kind: domain.ActorAgent, Name: "codex"}
	ticket := domain.Ticket{
		Ref: "ABC-1", ProjectKey: "ABC", FeatureRef: "ABC-F1",
		Type: domain.TicketTypeTask, Title: "T", Status: domain.WorkflowStatusBacklog,
		Priority:  domain.PriorityMedium,
		Assignee:  &assignee,
		Creator:   &creator,
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
	if fields["assignee"] != assignee.String() {
		t.Errorf("ticketDetail JSON assignee = %v, want %q", fields["assignee"], assignee.String())
	}
	if fields["creator"] != creator.String() {
		t.Errorf("ticketDetail JSON creator = %v, want %q", fields["creator"], creator.String())
	}
	if _, ok := fields["deleted_at"]; ok {
		t.Errorf("ticketDetail JSON has a \"deleted_at\" key: %s", b)
	}
}

// schemasWhereIDIsThePublicIdentity are the deliberate exceptions to
// ADR 0002's "no bare surrogate key on the wire" rule: a schema whose
// underlying row sits outside the entities registry (ADR 0002) and so
// has no ref/uuid to hide the surrogate behind at all — its sequential
// id is the real public identity, not a leaked internal detail. Every
// entry needs the same justification domain.Comment.ID's doc comment
// gives: no other stable identifier exists for a caller to name the
// row by.
//
// AgentTokenSummary/AgentTokenCreated: agent_tokens has no uuid column
// (like comments, actors, sessions, human_accounts — none of the
// identity tables are entities-registry rows), and its id is exactly
// what DELETE /agents/{name}/tokens/{id} names to revoke a specific
// token (product spec §4.1: "one or more revocable API tokens").
//
// Comment: the original documented exception this rule always had
// (domain.Comment.ID's own doc comment) — comments keep a dedicated
// INTEGER PRIMARY KEY instead of an entities-registry row (migration
// 0002_core_domain.sql), so there is no formatted reference or UUID
// for a surrogate to hide behind; id is the actual public identity.
// Wired up over HTTP as of Step 11.
//
// ExternalLink: the same shape as Comment, Phase 4 — external_links
// (migration 0006_external_links.sql) is also a dedicated
// INTEGER PRIMARY KEY table outside the entities registry, and its id
// is exactly what DELETE .../links/{id} names to remove one link.
var schemasWhereIDIsThePublicIdentity = map[string]bool{
	"AgentTokenSummary": true,
	"AgentTokenCreated": true,
	"Comment":           true,
	"ExternalLink":      true,
}

// TestNoSchemaExposesABareIntegerID is ADR 0002's assigned Phase 1
// test: no api/openapi.yaml component schema may declare an "id"
// property typed as a bare integer — internal surrogate keys
// (entities.id and friends) must never reach the wire; only a
// formatted reference, a project key, a UUID, or one of
// schemasWhereIDIsThePublicIdentity's deliberate exceptions may.
func TestNoSchemaExposesABareIntegerID(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("load openapi.yaml: %v", err)
	}

	for name, schemaRef := range doc.Components.Schemas {
		if schemasWhereIDIsThePublicIdentity[name] {
			continue
		}
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
