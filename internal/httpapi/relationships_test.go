package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestRelationshipLifecycleOverHTTP exercises add/list/remove for
// ticket relationships, each validated against api/openapi.yaml.
func TestRelationshipLifecycleOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]any{"type": "task", "title": "A", "general": true}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]any{"type": "task", "title": "B", "general": true}))

	addResp, addBody := ts.do(http.MethodPost, "/tickets/ABC-1/relationships", nil,
		mustJSON(t, map[string]string{"target": "ABC-2", "type": "blocks"}))
	if addResp.StatusCode != http.StatusCreated {
		t.Fatalf("add relationship status = %d, body=%s", addResp.StatusCode, addBody)
	}

	listResp, listBody := ts.do(http.MethodGet, "/tickets/ABC-1/relationships", nil, nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list relationships status = %d, body=%s", listResp.StatusCode, listBody)
	}
	var page map[string]any
	_ = json.Unmarshal(listBody, &page)
	rels, _ := page["relationships"].([]any)
	if len(rels) != 1 {
		t.Fatalf("relationships for ABC-1 = %d, want 1", len(rels))
	}
	rel, _ := rels[0].(map[string]any)
	if rel["type"] != "blocks" || rel["other"] != "ABC-2" {
		t.Errorf("relationship = %+v, want type=blocks other=ABC-2", rel)
	}

	// The other side sees the inverse view.
	otherListResp, otherListBody := ts.do(http.MethodGet, "/tickets/ABC-2/relationships", nil, nil)
	if otherListResp.StatusCode != http.StatusOK {
		t.Fatalf("list relationships (other side) status = %d, body=%s", otherListResp.StatusCode, otherListBody)
	}
	var otherPage map[string]any
	_ = json.Unmarshal(otherListBody, &otherPage)
	otherRels, _ := otherPage["relationships"].([]any)
	if len(otherRels) != 1 {
		t.Fatalf("relationships for ABC-2 = %d, want 1", len(otherRels))
	}
	otherRel, _ := otherRels[0].(map[string]any)
	if otherRel["type"] != "blocked_by" || otherRel["other"] != "ABC-1" {
		t.Errorf("inverse relationship = %+v, want type=blocked_by other=ABC-1", otherRel)
	}

	// --- duplicate add is already_exists ---
	dupResp, dupBody := ts.do(http.MethodPost, "/tickets/ABC-1/relationships", nil,
		mustJSON(t, map[string]string{"target": "ABC-2", "type": "blocks"}))
	if dupResp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate relationship status = %d, want 409, body=%s", dupResp.StatusCode, dupBody)
	}

	// --- remove ---
	removeResp, removeBody := ts.do(http.MethodDelete, "/tickets/ABC-1/relationships/blocks/ABC-2", nil, nil)
	if removeResp.StatusCode != http.StatusOK {
		t.Fatalf("remove relationship status = %d, body=%s", removeResp.StatusCode, removeBody)
	}

	afterResp, afterBody := ts.do(http.MethodGet, "/tickets/ABC-1/relationships", nil, nil)
	if afterResp.StatusCode != http.StatusOK {
		t.Fatalf("list relationships after remove status = %d, body=%s", afterResp.StatusCode, afterBody)
	}
	var afterPage map[string]any
	_ = json.Unmarshal(afterBody, &afterPage)
	if afterRels, _ := afterPage["relationships"].([]any); len(afterRels) != 0 {
		t.Errorf("relationships after remove = %d, want 0", len(afterRels))
	}
}

// TestRelationshipCycleRejectedOverHTTP is the first place
// relationship_cycle is exercised over HTTP: parent_of/blocks cycles
// are rejected (ADR 0014).
func TestRelationshipCycleRejectedOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]any{"type": "task", "title": "A", "general": true}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]any{"type": "task", "title": "B", "general": true}))

	resp, body := ts.do(http.MethodPost, "/tickets/ABC-1/relationships", nil,
		mustJSON(t, map[string]string{"target": "ABC-2", "type": "blocks"}))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first add status = %d, body=%s", resp.StatusCode, body)
	}

	// ABC-2 blocking ABC-1 back would create a cycle.
	cycleResp, cycleBody := ts.do(http.MethodPost, "/tickets/ABC-2/relationships", nil,
		mustJSON(t, map[string]string{"target": "ABC-1", "type": "blocks"}))
	if cycleResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("cycle-creating add status = %d, want 400, body=%s", cycleResp.StatusCode, cycleBody)
	}
	var envelope map[string]any
	_ = json.Unmarshal(cycleBody, &envelope)
	errObj, _ := envelope["error"].(map[string]any)
	if errObj["code"] != "relationship_cycle" {
		t.Errorf("error.code = %v, want relationship_cycle", errObj["code"])
	}
}

// TestAssociationLifecycleOverHTTP exercises add/list/remove for the
// looser associated_with edge, including a ticket-feature pairing
// (the one edge kind that spans both entity kinds).
func TestAssociationLifecycleOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]any{"type": "task", "title": "A", "general": true}))
	ts.do(http.MethodPost, "/projects/ABC/features", nil, mustJSON(t, map[string]string{"title": "Payments", "priority": "medium"}))

	addResp, addBody := ts.do(http.MethodPost, "/tickets/ABC-1/associations", nil,
		mustJSON(t, map[string]string{"target": "ABC-F2"}))
	if addResp.StatusCode != http.StatusCreated {
		t.Fatalf("add association status = %d, body=%s", addResp.StatusCode, addBody)
	}

	listResp, listBody := ts.do(http.MethodGet, "/tickets/ABC-1/associations", nil, nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list associations status = %d, body=%s", listResp.StatusCode, listBody)
	}
	var page map[string]any
	_ = json.Unmarshal(listBody, &page)
	associated, _ := page["associated"].([]any)
	if len(associated) != 1 || associated[0] != "ABC-F2" {
		t.Errorf("associated = %v, want [ABC-F2]", associated)
	}

	// The feature side sees the same edge.
	featureListResp, featureListBody := ts.do(http.MethodGet, "/features/ABC-F2/associations", nil, nil)
	if featureListResp.StatusCode != http.StatusOK {
		t.Fatalf("list feature associations status = %d, body=%s", featureListResp.StatusCode, featureListBody)
	}
	var featurePage map[string]any
	_ = json.Unmarshal(featureListBody, &featurePage)
	featureAssociated, _ := featurePage["associated"].([]any)
	if len(featureAssociated) != 1 || featureAssociated[0] != "ABC-1" {
		t.Errorf("feature-side associated = %v, want [ABC-1]", featureAssociated)
	}

	// --- duplicate add is already_exists ---
	dupResp, dupBody := ts.do(http.MethodPost, "/tickets/ABC-1/associations", nil,
		mustJSON(t, map[string]string{"target": "ABC-F2"}))
	if dupResp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate association status = %d, want 409, body=%s", dupResp.StatusCode, dupBody)
	}

	// --- remove, from the feature side ---
	removeResp, removeBody := ts.do(http.MethodDelete, "/features/ABC-F2/associations/ABC-1", nil, nil)
	if removeResp.StatusCode != http.StatusOK {
		t.Fatalf("remove association status = %d, body=%s", removeResp.StatusCode, removeBody)
	}

	afterResp, afterBody := ts.do(http.MethodGet, "/tickets/ABC-1/associations", nil, nil)
	if afterResp.StatusCode != http.StatusOK {
		t.Fatalf("list associations after remove status = %d, body=%s", afterResp.StatusCode, afterBody)
	}
	var afterPage map[string]any
	_ = json.Unmarshal(afterBody, &afterPage)
	if afterAssociated, _ := afterPage["associated"].([]any); len(afterAssociated) != 0 {
		t.Errorf("associated after remove = %v, want []", afterAssociated)
	}
}
