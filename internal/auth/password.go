package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters used by HashPassword. Encoded into every hash it
// produces (see the format comment below), so changing these constants
// only affects hashes created from now on - VerifyPassword reads each
// hash's own embedded parameters rather than assuming today's
// constants, so passwords hashed under an older tuning keep verifying
// correctly after this changes.
const (
	argonIterations = 1
	argonMemoryKiB  = 64 * 1024 // 64 MiB
	argonThreads    = 4
	argonKeyLen     = 32
	argonSaltLen    = 16
)

// HashPassword returns a self-describing Argon2id hash of plain, in
// the common PHC-string form
// "$argon2id$v=<version>$m=<memory>,t=<iterations>,p=<threads>$<salt>$<hash>"
// (base64, unpadded), so the parameters travel with the hash rather
// than needing to match a global constant forever (product spec §10:
// "Argon2id with per-password salts").
func HashPassword(plain string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}
	hash := argon2.IDKey([]byte(plain), salt, argonIterations, argonMemoryKiB, argonThreads, argonKeyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonIterations, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword reports whether plain matches encoded, a hash
// HashPassword produced. It compares in constant time and derives the
// candidate hash using encoded's own embedded parameters rather than
// this package's current constants.
func VerifyPassword(encoded, plain string) (bool, error) {
	memory, iterations, threads, salt, hash, err := parseEncodedHash(encoded)
	if err != nil {
		return false, err
	}
	candidate := argon2.IDKey([]byte(plain), salt, iterations, memory, threads, uint32(len(hash)))
	return subtle.ConstantTimeCompare(candidate, hash) == 1, nil
}

func parseEncodedHash(encoded string) (memory, iterations uint32, threads uint8, salt, hash []byte, err error) {
	// Splitting "$argon2id$v=19$m=..,t=..,p=..$salt$hash" on "$" yields
	// ["", "argon2id", "v=19", "m=..,t=..,p=..", "salt", "hash"].
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return 0, 0, 0, nil, nil, fmt.Errorf("auth: not a recognized argon2id hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("auth: parse hash version: %w", err)
	}
	if version != argon2.Version {
		return 0, 0, 0, nil, nil, fmt.Errorf("auth: unsupported argon2 version %d", version)
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("auth: parse hash params: %w", err)
	}
	if salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("auth: decode salt: %w", err)
	}
	if hash, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("auth: decode hash: %w", err)
	}
	return memory, iterations, threads, salt, hash, nil
}
