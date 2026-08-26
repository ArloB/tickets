package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestGetProjectBriefOverHTTP confirms GET /projects/{key}/brief
// returns every section the response schema promises, validated
// against api/openapi.yaml by ts.do itself.
func TestGetProjectBriefOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	ts.do(http.MethodPost, "/projects", nil, mustJSON(t, map[string]string{"key": "ABC", "title": "Example"}))
	ts.do(http.MethodPost, "/projects/ABC/tickets", nil, mustJSON(t, map[string]any{"type": "task", "title": "A ticket", "general": true}))

	resp, body := ts.do(http.MethodGet, "/projects/ABC/brief", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body=%s", resp.StatusCode, body)
	}

	var brief struct {
		Project struct {
			Key string `json:"key"`
		} `json:"project"`
		InProgress    []map[string]any `json:"in_progress"`
		IssueRegister []map[string]any `json:"issue_register"`
		Features      []map[string]any `json:"features"`
	}
	if err := json.Unmarshal(body, &brief); err != nil {
		t.Fatalf("unmarshal brief: %v", err)
	}
	if brief.Project.Key != "ABC" {
		t.Errorf("brief.Project.Key = %q, want %q", brief.Project.Key, "ABC")
	}
	if len(brief.InProgress) != 1 {
		t.Errorf("len(InProgress) = %d, want 1", len(brief.InProgress))
	}
	if len(brief.Features) != 1 {
		t.Errorf("len(Features) = %d, want 1 (General)", len(brief.Features))
	}
	if td, ok := brief.Features[0]["tickets_total"].(float64); !ok || td != 1 {
		t.Errorf("Features[0].tickets_total = %v, want 1", brief.Features[0]["tickets_total"])
	}
}

func TestGetProjectBriefNotFoundOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	resp, _ := ts.do(http.MethodGet, "/projects/ZZZ/brief", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
