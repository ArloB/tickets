package auth

import "testing"

func TestGenerateTokenRawNeverEqualsHash(t *testing.T) {
	raw, hash, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if raw == "" || hash == "" {
		t.Fatalf("GenerateToken returned empty raw/hash: %q %q", raw, hash)
	}
	if raw == hash {
		t.Error("raw token equals its own hash")
	}
	if HashToken(raw) != hash {
		t.Error("HashToken(raw) != the hash GenerateToken returned")
	}
}

func TestGenerateTokenIsRandom(t *testing.T) {
	raw1, _, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	raw2, _, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if raw1 == raw2 {
		t.Error("two generated tokens are identical")
	}
}

func TestHashTokenIsDeterministic(t *testing.T) {
	first := HashToken("some-raw-value")
	second := HashToken("some-raw-value")
	if first != second {
		t.Errorf("HashToken(%q) = %q then %q, want identical results for identical input", "some-raw-value", first, second)
	}
}
