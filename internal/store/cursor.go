package store

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// EncodeCursor packs an ordered tuple of pagination-position components
// into an opaque token. It generalizes the (created_at, id) shape a
// simple list query needs to whatever ordered tuple a richer query's
// ORDER BY / row-value WHERE comparison uses — e.g. (priority_rank,
// position, created_at, id) for the priority queue. Callers supply
// components in the exact order their query compares them in;
// EncodeCursor itself doesn't know or care what they mean.
func EncodeCursor(parts ...string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "|")))
}

// DecodeCursor is EncodeCursor's inverse. wantParts is the exact
// component count the caller expects; a mismatch is a malformed-cursor
// error rather than silently reading the wrong fields (e.g. a
// priority-queue cursor handed to a plain list's decoder). An empty
// cursor decodes to wantParts empty strings — the zero/first-page
// position for every component.
func DecodeCursor(cursor string, wantParts int) ([]string, error) {
	if cursor == "" {
		return make([]string, wantParts), nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf("decode cursor: %w", err)
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != wantParts {
		return nil, fmt.Errorf("malformed cursor: got %d components, want %d", len(parts), wantParts)
	}
	return parts, nil
}

// EncodeCreatedAtIDCursor and DecodeCreatedAtIDCursor are the typed
// convenience wrapper around the (created_at, id) shape every simple
// list query (projects, and Phase 1's features/comments/audit events)
// uses — the common case, kept ergonomic rather than making every
// caller juggle []string and strconv by hand.
func EncodeCreatedAtIDCursor(createdAt string, id int64) string {
	return EncodeCursor(createdAt, strconv.FormatInt(id, 10))
}

func DecodeCreatedAtIDCursor(cursor string) (createdAt string, id int64, err error) {
	parts, err := DecodeCursor(cursor, 2)
	if err != nil {
		return "", 0, err
	}
	if parts[1] == "" {
		return parts[0], 0, nil
	}
	id, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("malformed cursor id: %w", err)
	}
	return parts[0], id, nil
}
