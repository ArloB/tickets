package service

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// TestActivityEventTypesMatchOpenAPIEnum is Phase 6 Step 11's guard
// for a gap docs/mvp-acceptance.md's "Accepted for the MVP" section
// flagged in Step 7: api/openapi.yaml's ActivityEventType schema
// states in its own description that activityEventTypes is its
// "single source of truth ... keep the two in lockstep," but nothing
// enforced that — the drift (missing attachment_added/
// attachment_replaced/attachment_removed) was only caught incidentally
// via an unrelated response-validation test. This test fails directly
// and specifically the next time the two sets diverge, instead of
// relying on some other test happening to exercise the missing value.
func TestActivityEventTypesMatchOpenAPIEnum(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("load openapi.yaml: %v", err)
	}
	schema, ok := doc.Components.Schemas["ActivityEventType"]
	if !ok || schema.Value == nil {
		t.Fatalf("api/openapi.yaml has no ActivityEventType component schema")
	}

	fromOpenAPI := map[string]bool{}
	for _, v := range schema.Value.Enum {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("ActivityEventType enum value %v is not a string", v)
		}
		fromOpenAPI[s] = true
	}

	missingFromOpenAPI := diffKeys(activityEventTypes, fromOpenAPI)
	missingFromCode := diffKeys(fromOpenAPI, activityEventTypes)
	if len(missingFromOpenAPI) > 0 {
		t.Errorf("activityEventTypes has event type(s) not in api/openapi.yaml's ActivityEventType enum: %v", missingFromOpenAPI)
	}
	if len(missingFromCode) > 0 {
		t.Errorf("api/openapi.yaml's ActivityEventType enum has value(s) not in activityEventTypes: %v", missingFromCode)
	}
}

// diffKeys returns the keys present in a but not in b, sorted for a
// stable, readable failure message.
func diffKeys(a, b map[string]bool) []string {
	var diff []string
	for k := range a {
		if !b[k] {
			diff = append(diff, k)
		}
	}
	sort.Strings(diff)
	return diff
}
