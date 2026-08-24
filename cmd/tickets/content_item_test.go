package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// contentItemCLICases lets the lifecycle/idempotency/validation tests
// below run once per subcommand (plan, document) rather than being
// duplicated — mirrors decision_test.go's cases, but table-driven since
// runPlan/runDocument only differ in which function and reference
// prefix they use.
var contentItemCLICases = []struct {
	name string
	run  func([]string) error
}{
	{"plan", runPlan},
	{"document", runDocument},
}

func TestContentItemCreateGetUpdateLifecycle(t *testing.T) {
	for _, tc := range contentItemCLICases {
		t.Run(tc.name, func(t *testing.T) {
			isolateClientEnv(t)
			apiURL, token, _ := newTestAPIServerWithAgent(t)
			t.Setenv("TICKETS_API_TOKEN", token)
			t.Setenv("TICKETS_PROJECT", "ABC")

			createOut := captureStdout(t, func() {
				if err := tc.run([]string{
					"create", "--url", apiURL, "--title", "Rollout", "--body", "Step one", "--json",
				}); err != nil {
					t.Fatalf("run create: %v", err)
				}
			})
			var created map[string]any
			if err := json.Unmarshal([]byte(createOut), &created); err != nil {
				t.Fatalf("decode create --json output: %v (raw: %s)", err, createOut)
			}
			ref, _ := created["ref"].(string)
			if ref == "" || created["body"] != "Step one" {
				t.Fatalf("create output = %v, want a ref and body=%q", created, "Step one")
			}

			getOut := captureStdout(t, func() {
				if err := tc.run([]string{"get", ref, "--url", apiURL, "--json"}); err != nil {
					t.Fatalf("run get: %v", err)
				}
			})
			if !strings.Contains(getOut, "Rollout") {
				t.Errorf("get output = %q, want it to contain the title", getOut)
			}

			updateOut := captureStdout(t, func() {
				if err := tc.run([]string{
					"update", ref, "--url", apiURL, "--title", "Rollout (final)", "--if-version", "1", "--json",
				}); err != nil {
					t.Fatalf("run update: %v", err)
				}
			})
			var updated map[string]any
			if err := json.Unmarshal([]byte(updateOut), &updated); err != nil {
				t.Fatalf("decode update --json output: %v (raw: %s)", err, updateOut)
			}
			if updated["title"] != "Rollout (final)" {
				t.Errorf("update output = %v, want title=%q", updated, "Rollout (final)")
			}
			if updated["body"] != "Step one" {
				t.Errorf("update omitted --body, want it preserved from the current value; got %v", updated)
			}
		})
	}
}

// TestContentItemCreateIdempotencyKeyIsWired mirrors
// TestDecisionCreateIdempotencyKeyIsWired.
func TestContentItemCreateIdempotencyKeyIsWired(t *testing.T) {
	for _, tc := range contentItemCLICases {
		t.Run(tc.name, func(t *testing.T) {
			isolateClientEnv(t)
			apiURL, token, _ := newTestAPIServerWithAgent(t)
			t.Setenv("TICKETS_API_TOKEN", token)
			t.Setenv("TICKETS_PROJECT", "ABC")

			runCreate := func() map[string]any {
				out := captureStdout(t, func() {
					if err := tc.run([]string{
						"create", "--url", apiURL, "--title", "Rollout", "--idempotency-key", "dup-key-1", "--json",
					}); err != nil {
						t.Fatalf("run create: %v", err)
					}
				})
				var m map[string]any
				if err := json.Unmarshal([]byte(out), &m); err != nil {
					t.Fatalf("decode create --json output: %v (raw: %s)", err, out)
				}
				return m
			}

			first := runCreate()
			replay := runCreate()
			if first["ref"] == "" || first["ref"] != replay["ref"] {
				t.Errorf("create replayed with the same --idempotency-key: refs = %v, %v — want the same ref", first["ref"], replay["ref"])
			}
		})
	}
}

func TestContentItemListRequiresProject(t *testing.T) {
	for _, tc := range contentItemCLICases {
		t.Run(tc.name, func(t *testing.T) {
			isolateClientEnv(t)
			apiURL, token, _ := newTestAPIServerWithAgent(t)
			t.Setenv("TICKETS_API_TOKEN", token)

			if err := tc.run([]string{"list", "--url", apiURL}); err == nil {
				t.Error("list with no --project: want error, got nil")
			}
		})
	}
}

func TestContentItemCreateRequiresTitle(t *testing.T) {
	for _, tc := range contentItemCLICases {
		t.Run(tc.name, func(t *testing.T) {
			isolateClientEnv(t)
			apiURL, token, _ := newTestAPIServerWithAgent(t)
			t.Setenv("TICKETS_API_TOKEN", token)
			t.Setenv("TICKETS_PROJECT", "ABC")

			if err := tc.run([]string{"create", "--url", apiURL}); err == nil {
				t.Error("create with no --title: want error, got nil")
			}
		})
	}
}

func TestContentItemCreateRejectsBothInlineAndFile(t *testing.T) {
	for _, tc := range contentItemCLICases {
		t.Run(tc.name, func(t *testing.T) {
			isolateClientEnv(t)
			apiURL, token, _ := newTestAPIServerWithAgent(t)
			t.Setenv("TICKETS_API_TOKEN", token)
			t.Setenv("TICKETS_PROJECT", "ABC")

			err := tc.run([]string{
				"create", "--url", apiURL, "--title", "T", "--body", "inline", "--body-file", "-",
			})
			if err == nil {
				t.Error("create with both --body and --body-file: want error, got nil")
			}
		})
	}
}

func TestContentItemUpdateRequiresFullRepresentation(t *testing.T) {
	for _, tc := range contentItemCLICases {
		t.Run(tc.name, func(t *testing.T) {
			isolateClientEnv(t)
			apiURL, token, _ := newTestAPIServerWithAgent(t)
			t.Setenv("TICKETS_API_TOKEN", token)

			if err := tc.run([]string{"update", "ABC-P1", "--url", apiURL, "--if-version", "1"}); err == nil {
				t.Error("update with no --title: want error, got nil")
			}
			if err := tc.run([]string{"update", "ABC-P1", "--url", apiURL, "--title", "x"}); err == nil {
				t.Error("update with no --if-version: want error, got nil")
			}
		})
	}
}

func TestContentItemRequiresSubcommand(t *testing.T) {
	for _, tc := range contentItemCLICases {
		if err := tc.run(nil); err == nil {
			t.Errorf("%s with no subcommand: want error, got nil", tc.name)
		}
	}
}

func TestContentItemRejectsUnknownSubcommand(t *testing.T) {
	for _, tc := range contentItemCLICases {
		if err := tc.run([]string{"not-a-real-subcommand"}); err == nil {
			t.Errorf("%s with an unknown subcommand: want error, got nil", tc.name)
		}
	}
}

// TestContentItemVersionsAndDiffCLI mirrors decision_versions_test.go's
// CLI coverage, run once against the plan subcommand (versions/diff
// logic is identical between plan and document — see
// runContentItemVersions/runContentItemDiff).
func TestContentItemVersionsAndDiffCLI(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)
	t.Setenv("TICKETS_PROJECT", "ABC")

	createOut := captureStdout(t, func() {
		if err := runPlan([]string{"create", "--url", apiURL, "--title", "P", "--body", "line one", "--json"}); err != nil {
			t.Fatalf("runPlan create: %v", err)
		}
	})
	var created map[string]any
	_ = json.Unmarshal([]byte(createOut), &created)
	ref, _ := created["ref"].(string)

	if err := runPlan([]string{"update", ref, "--url", apiURL, "--title", "P", "--body", "line one\nline two", "--if-version", "1"}); err != nil {
		t.Fatalf("runPlan update: %v", err)
	}

	versionsOut := captureStdout(t, func() {
		if err := runPlan([]string{"versions", ref, "--url", apiURL, "--json"}); err != nil {
			t.Fatalf("runPlan versions: %v", err)
		}
	})
	if !strings.Contains(versionsOut, `"version": 1`) {
		t.Errorf("versions output = %q, want it to contain the archived version 1", versionsOut)
	}

	diffOut := captureStdout(t, func() {
		if err := runPlan([]string{"diff", ref, "--url", apiURL, "--from", "1", "--to", "2"}); err != nil {
			t.Fatalf("runPlan diff: %v", err)
		}
	})
	if !strings.Contains(diffOut, "line two") {
		t.Errorf("diff output = %q, want it to show the added line", diffOut)
	}
}

func TestContentItemDiffRequiresFromAndTo(t *testing.T) {
	isolateClientEnv(t)
	apiURL, token, _ := newTestAPIServerWithAgent(t)
	t.Setenv("TICKETS_API_TOKEN", token)

	if err := runPlan([]string{"diff", "ABC-P1", "--url", apiURL}); err == nil {
		t.Error("plan diff with no --from/--to: want error, got nil")
	}
}
