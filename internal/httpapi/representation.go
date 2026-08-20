package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/service"
)

// fieldNames splits a comma-separated ?fields= query value into
// trimmed, non-empty names. An empty result means "no fields param"
// (the caller gets the fixed compact/detail shape unchanged) — the
// zero-length-vs-nil distinction doesn't matter to any caller here, so
// this returns nil for both "absent" and "present but empty".
func fieldNames(r *http.Request) []string {
	raw := r.URL.Query().Get("fields")
	if raw == "" {
		return nil
	}
	var out []string
	for _, f := range strings.Split(raw, ",") {
		f = strings.TrimSpace(f)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// includeNames is fieldNames' counterpart for ?include=.
func includeNames(r *http.Request) map[string]bool {
	raw := r.URL.Query().Get("include")
	if raw == "" {
		return nil
	}
	out := make(map[string]bool)
	for _, f := range strings.Split(raw, ",") {
		f = strings.TrimSpace(f)
		if f != "" {
			out[f] = true
		}
	}
	return out
}

// validateFieldNames checks every requested field name against a
// per-DTO allow-list before any projection happens — an unknown name
// is validation_failed, not silently ignored. A per-DTO allow-list,
// not reflection over the struct: allowedTicketCompactFields and
// allowedTicketDetailFields are two different answers for the same
// query string, matching the plan's explicit design call ("fields=
// creator on a compact response and fields=creator on a detail
// response are different answers").
func validateFieldNames(allowed map[string]bool, fields []string) *service.Error {
	for _, f := range fields {
		if !allowed[f] {
			return &service.Error{Code: domain.ErrValidationFailed, Field: "fields", Message: "unknown field " + strconv.Quote(f)}
		}
	}
	return nil
}

// projectFields narrows dto to exactly fields' named top-level JSON
// keys (docs/contracts/representations.md's fields= contract) —
// callers validate fields against a per-DTO allow-list once via
// validateFieldNames before calling this per item, rather than
// re-validating on every row of a list response.
//
// Implemented via a marshal/filter round trip through map[string]any
// rather than reflection over dto's struct tags — the codebase's
// stated aversion to large abstractions (product spec §8.2) argues
// against a generic struct-projection package for what is, today,
// exactly two call sites.
func projectFields(dto any, fields []string) (map[string]any, *service.Error) {
	b, err := json.Marshal(dto)
	if err != nil {
		return nil, &service.Error{Code: domain.ErrInternal, Message: "failed to project fields"}
	}
	var full map[string]any
	if err := json.Unmarshal(b, &full); err != nil {
		return nil, &service.Error{Code: domain.ErrInternal, Message: "failed to project fields"}
	}
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		if v, ok := full[f]; ok {
			out[f] = v
		}
	}
	return out, nil
}

var allowedTicketCompactFields = map[string]bool{
	"ref": true, "title": true, "type": true, "status": true,
	"priority": true, "severity": true, "updated_at": true, "version": true,
}

var allowedTicketDetailFields = map[string]bool{
	"ref": true, "project": true, "feature": true, "type": true, "title": true,
	"description": true, "status": true, "priority": true, "severity": true,
	"assignee": true, "creator": true, "version": true, "created_at": true, "updated_at": true,
	"comments": true, "relationships": true,
}
