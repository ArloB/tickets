package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestContentItemCreateFileRepresentationCLI(t *testing.T) {
	for _, tc := range contentItemCLICases {
		t.Run(tc.name, func(t *testing.T) {
			isolateClientEnv(t)
			apiURL, token, _ := newTestAPIServerWithAgent(t)
			t.Setenv("TICKETS_API_TOKEN", token)
			t.Setenv("TICKETS_PROJECT", "ABC")

			srcPath := filepath.Join(t.TempDir(), "spec.pdf")
			if err := os.WriteFile(srcPath, []byte("pdf bytes"), 0o644); err != nil {
				t.Fatalf("write source file: %v", err)
			}

			createOut := captureStdout(t, func() {
				if err := tc.run([]string{"create", "--url", apiURL, "--title", "Spec", "--file", srcPath, "--json"}); err != nil {
					t.Fatalf("run create: %v", err)
				}
			})
			var created map[string]any
			if err := json.Unmarshal([]byte(createOut), &created); err != nil {
				t.Fatalf("decode create --json output: %v (raw: %s)", err, createOut)
			}
			ref := created["ref"].(string)
			if created["representation"] != "file" {
				t.Errorf("representation = %v, want file", created["representation"])
			}
			if created["file_name"] != "spec.pdf" {
				t.Errorf("file_name = %v, want spec.pdf", created["file_name"])
			}

			destPath := filepath.Join(t.TempDir(), "downloaded.pdf")
			if err := tc.run([]string{"download", ref, "--url", apiURL, "--output", destPath}); err != nil {
				t.Fatalf("run download: %v", err)
			}
			got, err := os.ReadFile(destPath)
			if err != nil {
				t.Fatalf("read downloaded file: %v", err)
			}
			if string(got) != "pdf bytes" {
				t.Errorf("downloaded content = %q, want %q", got, "pdf bytes")
			}
		})
	}
}

func TestContentItemCreatePathRepresentationCLI(t *testing.T) {
	for _, tc := range contentItemCLICases {
		t.Run(tc.name, func(t *testing.T) {
			isolateClientEnv(t)
			apiURL, token, _ := newTestAPIServerWithAgent(t)
			t.Setenv("TICKETS_API_TOKEN", token)
			t.Setenv("TICKETS_PROJECT", "ABC")

			out := captureStdout(t, func() {
				if err := tc.run([]string{"create", "--url", apiURL, "--title", "External", "--path", "/srv/docs/x.md", "--json"}); err != nil {
					t.Fatalf("run create: %v", err)
				}
			})
			var created map[string]any
			if err := json.Unmarshal([]byte(out), &created); err != nil {
				t.Fatalf("decode create --json output: %v (raw: %s)", err, out)
			}
			if created["representation"] != "path" {
				t.Errorf("representation = %v, want path", created["representation"])
			}
			if created["path_value"] != "/srv/docs/x.md" {
				t.Errorf("path_value = %v, want /srv/docs/x.md", created["path_value"])
			}
		})
	}
}

func TestContentItemCreateURLRepresentationCLI(t *testing.T) {
	for _, tc := range contentItemCLICases {
		t.Run(tc.name, func(t *testing.T) {
			isolateClientEnv(t)
			apiURL, token, _ := newTestAPIServerWithAgent(t)
			t.Setenv("TICKETS_API_TOKEN", token)
			t.Setenv("TICKETS_PROJECT", "ABC")

			out := captureStdout(t, func() {
				if err := tc.run([]string{"create", "--url", apiURL, "--title", "Wiki", "--content-url", "https://wiki.example.com/x", "--json"}); err != nil {
					t.Fatalf("run create: %v", err)
				}
			})
			var created map[string]any
			if err := json.Unmarshal([]byte(out), &created); err != nil {
				t.Fatalf("decode create --json output: %v (raw: %s)", err, out)
			}
			if created["representation"] != "url" {
				t.Errorf("representation = %v, want url", created["representation"])
			}
			if created["url_value"] != "https://wiki.example.com/x" {
				t.Errorf("url_value = %v, want https://wiki.example.com/x", created["url_value"])
			}
		})
	}
}

func TestContentItemUpdatePathRepresentationCLI(t *testing.T) {
	for _, tc := range contentItemCLICases {
		t.Run(tc.name, func(t *testing.T) {
			isolateClientEnv(t)
			apiURL, token, _ := newTestAPIServerWithAgent(t)
			t.Setenv("TICKETS_API_TOKEN", token)
			t.Setenv("TICKETS_PROJECT", "ABC")

			createOut := captureStdout(t, func() {
				if err := tc.run([]string{"create", "--url", apiURL, "--title", "External", "--path", "/old.md", "--json"}); err != nil {
					t.Fatalf("run create: %v", err)
				}
			})
			var created map[string]any
			if err := json.Unmarshal([]byte(createOut), &created); err != nil {
				t.Fatalf("decode create output: %v", err)
			}
			ref := created["ref"].(string)

			updateOut := captureStdout(t, func() {
				if err := tc.run([]string{
					"update", ref, "--url", apiURL, "--title", "External", "--path", "/new.md",
					"--if-version", "1", "--json",
				}); err != nil {
					t.Fatalf("run update: %v", err)
				}
			})
			var updated map[string]any
			if err := json.Unmarshal([]byte(updateOut), &updated); err != nil {
				t.Fatalf("decode update output: %v", err)
			}
			if updated["path_value"] != "/new.md" {
				t.Errorf("path_value after update = %v, want /new.md", updated["path_value"])
			}
			if updated["representation"] != "path" {
				t.Errorf("representation after update = %v, want path", updated["representation"])
			}
		})
	}
}
