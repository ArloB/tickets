package store

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// EncodeCursor packs a (created_at, id) pagination position into an
// opaque token. Projects have no priority/position ordering (unlike
// tickets/features — product spec §5.6), so (created_at, id) is the
// natural, always-unique sort key: docs/contracts/representations.md.
func EncodeCursor(createdAt string, id int64) string {
	raw := createdAt + "|" + strconv.FormatInt(id, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor is EncodeCursor's inverse. An empty cursor decodes to
// the zero position (the first page).
func DecodeCursor(cursor string) (createdAt string, id int64, err error) {
	if cursor == "" {
		return "", 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", 0, fmt.Errorf("decode cursor: %w", err)
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("malformed cursor")
	}
	id, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("malformed cursor id: %w", err)
	}
	return parts[0], id, nil
}
