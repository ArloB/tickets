package auth

import "testing"

func TestHashAndVerifyPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	ok, err := VerifyPassword(hash, "correct horse battery staple")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Error("VerifyPassword: correct password rejected")
	}
}

func TestVerifyPasswordRejectsWrongPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	ok, err := VerifyPassword(hash, "wrong password")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if ok {
		t.Error("VerifyPassword: wrong password accepted")
	}
}

// TestHashPasswordSaltsDiffer guards against a fixed or reused salt,
// which would let two accounts sharing a password be spotted by
// comparing hashes (product spec §10: "per-password salts").
func TestHashPasswordSaltsDiffer(t *testing.T) {
	h1, err := HashPassword("same password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	h2, err := HashPassword("same password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if h1 == h2 {
		t.Error("two hashes of the same password are identical - salts must differ")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	if _, err := VerifyPassword("not-a-hash", "whatever"); err == nil {
		t.Error("VerifyPassword on a malformed hash: want error, got nil")
	}
}
