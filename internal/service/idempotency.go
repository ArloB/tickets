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

	"github.com/ArloB/tickets/internal/store"
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

// checkIdempotency looks up (key, fingerprint) inside tx. An empty key
// always returns ("", false, nil) — no idempotent-retry semantics
// requested. A prior record with a matching fingerprint returns the
// cached refKey (a project key or ticket ref, never a serialized
// snapshot — see the migration's comment on idempotency_keys.ref_key)
// so the caller re-fetches the live record. A prior record with a
// *different* fingerprint is idempotency_key_reused.
func checkIdempotency(ctx context.Context, tx *sql.Tx, key, fingerprint string) (refKey string, found bool, err error) {
	if key == "" {
		return "", false, nil
	}
	var existingFingerprint string
	err = tx.QueryRowContext(ctx,
		`SELECT fingerprint, ref_key FROM idempotency_keys WHERE key = ?`, key,
	).Scan(&existingFingerprint, &refKey)
	switch {
	case err == nil:
		if existingFingerprint != fingerprint {
			return "", false, newIdempotencyReusedError()
		}
		return refKey, true, nil
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	default:
		return "", false, fmt.Errorf("service: check idempotency key: %w", err)
	}
}

// recordIdempotency stores the mapping after a successful create. A
// no-op when key is empty.
func recordIdempotency(ctx context.Context, tx *sql.Tx, key, fingerprint, refKey string) error {
	if key == "" {
		return nil
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO idempotency_keys(key, fingerprint, ref_key, created_at) VALUES (?, ?, ?, ?)`,
		key, fingerprint, refKey, time.Now().UTC().Format(store.TimeLayout),
	); err != nil {
		return fmt.Errorf("service: store idempotency record: %w", err)
	}
	return nil
}
