package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Fingerprint implements docs/contracts/concurrency.md's canonical
// request fingerprint: SHA-256 over method, path, and the JSON body
// re-marshaled with sorted keys (encoding/json sorts map keys on
// Marshal), so two requests differing only in JSON key order produce
// the same fingerprint rather than a spurious idempotency_key_reused.
// internal/httpapi calls this — it owns method/path (ADR 0005).
func Fingerprint(method, path string, body []byte) (string, error) {
	var canonical any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &canonical); err != nil {
			return "", fmt.Errorf("service: body is not valid JSON: %w", err)
		}
	}
	canonBytes, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("service: canonicalize body: %w", err)
	}

	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte{'\n'})
	h.Write([]byte(path))
	h.Write([]byte{'\n'})
	h.Write(canonBytes)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// withIdempotency runs fn inside one transaction. If key is non-empty
// and a prior call with the same key and fingerprint already
// committed, it returns the cached result without calling fn again
// (docs/contracts/concurrency.md). A key reused with a *different*
// fingerprint is *Error{Code: domain.ErrIdempotencyKeyReused}.
//
// Correctness under concurrency relies on SQLite's serialized-writer
// transaction model (ADR 0003/0009), the same reasoning reference
// allocation depends on: two concurrent calls with the same key can't
// both observe "no existing row" and both proceed to insert.
func withIdempotency[T any](ctx context.Context, db *sql.DB, key, fingerprint string, fn func(tx *sql.Tx) (T, error)) (T, error) {
	var zero T

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return zero, fmt.Errorf("service: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if key != "" {
		var existingFingerprint, existingResult string
		err := tx.QueryRowContext(ctx,
			`SELECT fingerprint, result_json FROM idempotency_keys WHERE key = ?`, key,
		).Scan(&existingFingerprint, &existingResult)
		switch {
		case err == nil:
			if existingFingerprint != fingerprint {
				return zero, newIdempotencyReusedError()
			}
			var out T
			if err := json.Unmarshal([]byte(existingResult), &out); err != nil {
				return zero, fmt.Errorf("service: decode cached idempotent result: %w", err)
			}
			return out, nil // read-only path; rolls back via defer
		case errors.Is(err, sql.ErrNoRows):
			// no prior record; fall through to execute fn
		default:
			return zero, fmt.Errorf("service: check idempotency key: %w", err)
		}
	}

	result, err := fn(tx)
	if err != nil {
		return zero, err
	}

	if key != "" {
		resultBytes, err := json.Marshal(result)
		if err != nil {
			return zero, fmt.Errorf("service: marshal idempotent result: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO idempotency_keys(key, fingerprint, result_json, created_at) VALUES (?, ?, ?, ?)`,
			key, fingerprint, string(resultBytes), time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			return zero, fmt.Errorf("service: store idempotency record: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return zero, fmt.Errorf("service: commit: %w", err)
	}
	committed = true
	return result, nil
}
