package store

import (
	"context"
	"fmt"
)

// IntegrityCheck runs SQLite's own `PRAGMA integrity_check` — a
// full logical/physical page-level scan. ok is true only when the
// single-row result is the literal string "ok"; any other result
// (SQLite returns up to 100 problem descriptions before giving up)
// comes back as messages. `tickets admin integrity` (Phase 6 Step 3)
// is the only caller; this needs no transaction of its own since it's
// read-only and SQLite runs it as a special pragma query, not
// ordinary SQL.
func IntegrityCheck(ctx context.Context, q Querier) (ok bool, messages []string, err error) {
	rows, err := q.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return false, nil, fmt.Errorf("integrity check: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var msg string
		if err := rows.Scan(&msg); err != nil {
			return false, nil, fmt.Errorf("scan integrity_check row: %w", err)
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return false, nil, err
	}
	ok = len(messages) == 1 && messages[0] == "ok"
	return ok, messages, nil
}

// ForeignKeyViolation is one row PRAGMA foreign_key_check reports —
// a row whose foreign key doesn't resolve, which application-level
// code should never be able to produce (ADR 0003's foreign_keys=1
// pragma enforces this on every write already) but which a manual
// data edit, a bug in an old migration, or filesystem-level
// corruption could still introduce.
type ForeignKeyViolation struct {
	Table       string
	RowID       *int64
	ParentTable string
	// FKID identifies which of a table's possibly-several foreign
	// keys is violated, when the table declares more than one — not a
	// row identity.
	FKID int64
}

// ForeignKeyCheck runs `PRAGMA foreign_key_check`, cross-database
// (across every table, not scoped to one) — the check ADR 0003's
// always-on foreign_keys pragma performs incrementally on every
// write, run here as a full retrospective sweep.
func ForeignKeyCheck(ctx context.Context, q Querier) ([]ForeignKeyViolation, error) {
	rows, err := q.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return nil, fmt.Errorf("foreign key check: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ForeignKeyViolation
	for rows.Next() {
		var v ForeignKeyViolation
		if err := rows.Scan(&v.Table, &v.RowID, &v.ParentTable, &v.FKID); err != nil {
			return nil, fmt.Errorf("scan foreign_key_check row: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListReferencedBlobHashes returns every distinct non-null
// file_hash value referenced anywhere — the current row and the full
// archived-version history of both attachments and content items
// (plans/documents), since a prior version's blob must stay
// reachable through its own .../versions/{version}/download route
// (ADR 0013's "history stays visible" reasoning applied to files, not
// just field edits). `tickets admin integrity` (Phase 6 Step 3) diffs
// this against blobstore.Store.Hashes to find orphans — closing ADR
// 0007's open item ("a reasonable future addition ... isn't built
// speculatively here") as an operator-run check, not an automatic
// background sweep.
func ListReferencedBlobHashes(ctx context.Context, q Querier) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT file_hash FROM attachments WHERE file_hash IS NOT NULL
		UNION
		SELECT file_hash FROM attachment_versions WHERE file_hash IS NOT NULL
		UNION
		SELECT file_hash FROM content_items WHERE file_hash IS NOT NULL
		UNION
		SELECT file_hash FROM content_versions WHERE file_hash IS NOT NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("list referenced blob hashes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]bool{}
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, fmt.Errorf("scan file_hash row: %w", err)
		}
		out[hash] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
