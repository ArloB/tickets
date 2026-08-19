package store

import "testing"

func TestEncodeDecodeCursorRoundTrip(t *testing.T) {
	cases := [][]string{
		{"2026-01-01T00:00:00.000000000Z", "5"},
		{"0", "1000", "2026-01-01T00:00:00.000000000Z", "42"},
		{"1", "0", "1000", "2026-01-01T00:00:00.000000000Z", "42"},
	}
	for _, parts := range cases {
		token := EncodeCursor(parts...)
		got, err := DecodeCursor(token, len(parts))
		if err != nil {
			t.Errorf("DecodeCursor(EncodeCursor(%v)): %v", parts, err)
			continue
		}
		if len(got) != len(parts) {
			t.Fatalf("DecodeCursor returned %d parts, want %d", len(got), len(parts))
		}
		for i := range parts {
			if got[i] != parts[i] {
				t.Errorf("component %d = %q, want %q", i, got[i], parts[i])
			}
		}
	}
}

func TestDecodeCursorEmptyIsZeroPosition(t *testing.T) {
	got, err := DecodeCursor("", 4)
	if err != nil {
		t.Fatalf("DecodeCursor(\"\", 4): %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("DecodeCursor(\"\", 4) returned %d parts, want 4", len(got))
	}
	for i, p := range got {
		if p != "" {
			t.Errorf("component %d = %q, want empty string", i, p)
		}
	}
}

func TestDecodeCursorWrongComponentCount(t *testing.T) {
	token := EncodeCursor("a", "b", "c")
	if _, err := DecodeCursor(token, 2); err == nil {
		t.Error("DecodeCursor with mismatched wantParts = nil error, want error")
	}
}

func TestDecodeCursorMalformedBase64(t *testing.T) {
	if _, err := DecodeCursor("not valid base64!!", 2); err == nil {
		t.Error("DecodeCursor(malformed base64) = nil error, want error")
	}
}

func TestCreatedAtIDCursorRoundTrip(t *testing.T) {
	token := EncodeCreatedAtIDCursor("2026-01-01T00:00:00.000000000Z", 42)
	createdAt, id, err := DecodeCreatedAtIDCursor(token)
	if err != nil {
		t.Fatalf("DecodeCreatedAtIDCursor: %v", err)
	}
	if createdAt != "2026-01-01T00:00:00.000000000Z" || id != 42 {
		t.Errorf("got (%q, %d), want (%q, 42)", createdAt, id, "2026-01-01T00:00:00.000000000Z")
	}
}

func TestDecodeCreatedAtIDCursorEmpty(t *testing.T) {
	createdAt, id, err := DecodeCreatedAtIDCursor("")
	if err != nil {
		t.Fatalf("DecodeCreatedAtIDCursor(\"\"): %v", err)
	}
	if createdAt != "" || id != 0 {
		t.Errorf("got (%q, %d), want (\"\", 0)", createdAt, id)
	}
}
