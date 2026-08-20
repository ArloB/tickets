package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// GenerateToken returns a fresh, unguessable random token and its
// hash. ADR 0004: only a token's hash is ever stored; the raw value is
// shown once, at creation. Agent bearer tokens and session ids
// (migration 0004's comment on sessions.id explains why sessions are
// nonetheless stored raw, not by this hash) both start from the same
// generation.
func GenerateToken() (raw, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("auth: generate token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, HashToken(raw), nil
}

// HashToken hashes a raw token the same way GenerateToken's own hash
// return value does, for verifying a presented token against a stored
// hash (store.GetAgentTokenByHash's lookup key).
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
