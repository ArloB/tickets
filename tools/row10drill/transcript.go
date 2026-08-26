package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const toolPrefix = "mcp__tickets__"

type toolCall struct {
	Tool  string `json:"tool"`
	Error string `json:"error,omitempty"`
}

var domainErrorCodes = []string{
	"validation_failed",
	"not_found",
	"already_exists",
	"version_conflict",
	"idempotency_key_reused",
	"unauthorized",
	"forbidden",
	"throttled",
	"internal_error",
}

func (c toolCall) isSchemaError() bool {
	if c.Error == "" {
		return false
	}
	lower := strings.ToLower(c.Error)
	for _, code := range domainErrorCodes {
		if strings.Contains(lower, code) {
			return false
		}
	}
	return true
}

type transcript struct {
	Calls     []toolCall `json:"calls"`
	HostError string     `json:"host_error,omitempty"`
}

func (t transcript) sequence() []string {
	names := make([]string, 0, len(t.Calls))
	for _, c := range t.Calls {
		names = append(names, c.Tool)
	}
	return names
}

func (t transcript) errorCount() int {
	n := 0
	for _, c := range t.Calls {
		if c.Error != "" {
			n++
		}
	}
	return n
}

func (t transcript) schemaErrors() []toolCall {
	var out []toolCall
	for _, c := range t.Calls {
		if c.isSchemaError() {
			out = append(out, c)
		}
	}
	return out
}

func (t transcript) firstCall() string {
	if len(t.Calls) == 0 {
		return ""
	}
	return t.Calls[0].Tool
}

func scanJSONL(r io.Reader, fn func(map[string]any) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if err := fn(e); err != nil {
			return err
		}
	}
	return sc.Err()
}

func parseClaudeTranscript(r io.Reader) (transcript, error) {
	var t transcript
	pending := map[string]int{}

	err := scanJSONL(r, func(e map[string]any) error {
		switch e["type"] {
		case "assistant":
			msg, _ := e["message"].(map[string]any)
			content, _ := msg["content"].([]any)
			for _, raw := range content {
				block, _ := raw.(map[string]any)
				if block["type"] != "tool_use" {
					continue
				}
				name, _ := block["name"].(string)
				if !strings.HasPrefix(name, toolPrefix) {
					continue
				}
				id, _ := block["id"].(string)
				t.Calls = append(t.Calls, toolCall{Tool: strings.TrimPrefix(name, toolPrefix)})
				if id != "" {
					pending[id] = len(t.Calls) - 1
				}
			}
		case "user":
			msg, _ := e["message"].(map[string]any)
			content, _ := msg["content"].([]any)
			for _, raw := range content {
				block, _ := raw.(map[string]any)
				if block["type"] != "tool_result" {
					continue
				}
				isErr, _ := block["is_error"].(bool)
				if !isErr {
					continue
				}
				id, _ := block["tool_use_id"].(string)
				if idx, ok := pending[id]; ok {
					t.Calls[idx].Error = summarize(block["content"])
				}
			}
		case "result":
			if isErr, _ := e["is_error"].(bool); isErr {
				subtype, _ := e["subtype"].(string)
				t.HostError = fmt.Sprintf("claude reported failure: %s", subtype)
			}
		}
		return nil
	})
	return t, err
}

func parseCodexTranscript(r io.Reader) (transcript, error) {
	var t transcript

	err := scanJSONL(r, func(e map[string]any) error {
		if e["type"] != "item.completed" {
			return nil
		}
		item, _ := e["item"].(map[string]any)
		if item["type"] != "mcp_tool_call" {
			return nil
		}
		tool, _ := item["tool"].(string)
		call := toolCall{Tool: tool}
		if item["error"] != nil {
			call.Error = summarize(item["error"])
		}
		t.Calls = append(t.Calls, call)
		return nil
	})
	return t, err
}

func summarize(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return truncate(s)
	}
	if m, ok := v.(map[string]any); ok {
		if msg, ok := m["message"].(string); ok {
			return truncate(msg)
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "unreadable error payload"
	}
	return truncate(string(b))
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	if s == "" {
		return "unspecified error"
	}
	return s
}
