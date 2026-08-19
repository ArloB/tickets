package domain

import "testing"

func TestValidProjectKey(t *testing.T) {
	valid := []string{"ABC", "TIX9", "AB", "A123456789"}
	invalid := []string{"abc", "1BC", "A", "TOOLONGKEY1", "AB-C", "", "A_B"}

	for _, k := range valid {
		if !ValidProjectKey(k) {
			t.Errorf("ValidProjectKey(%q) = false, want true", k)
		}
	}
	for _, k := range invalid {
		if ValidProjectKey(k) {
			t.Errorf("ValidProjectKey(%q) = true, want false", k)
		}
	}
}

func TestFormat(t *testing.T) {
	cases := []struct {
		name string
		ref  Reference
		want string
	}{
		{"ticket", Reference{"ABC", KindTicket, 123}, "ABC-123"},
		{"feature", Reference{"ABC", KindFeature, 12}, "ABC-F12"},
		{"decision", Reference{"ABC", KindDecision, 7}, "ABC-D7"},
		{"plan", Reference{"ABC", KindPlan, 4}, "ABC-P4"},
		{"document", Reference{"ABC", KindDocument, 9}, "ABC-DOC9"},
		{"seq one", Reference{"ABC", KindTicket, 1}, "ABC-1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Format(c.ref)
			if err != nil {
				t.Fatalf("Format(%+v) unexpected error: %v", c.ref, err)
			}
			if got != c.want {
				t.Errorf("Format(%+v) = %q, want %q", c.ref, got, c.want)
			}
		})
	}
}

func TestFormatErrors(t *testing.T) {
	cases := []struct {
		name string
		ref  Reference
	}{
		{"invalid project key", Reference{"abc", KindTicket, 1}},
		{"unknown kind", Reference{"ABC", EntityKind("bogus"), 1}},
		{"zero sequence", Reference{"ABC", KindTicket, 0}},
		{"negative sequence", Reference{"ABC", KindTicket, -1}},
		{"project has no reference token", Reference{"ABC", KindProject, 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Format(c.ref); err == nil {
				t.Errorf("Format(%+v) = nil error, want error", c.ref)
			}
		})
	}
}

func TestEntityKindValid(t *testing.T) {
	valid := []EntityKind{KindProject, KindTicket, KindFeature, KindDecision, KindPlan, KindDocument}
	for _, k := range valid {
		if !k.Valid() {
			t.Errorf("%s.Valid() = false, want true", k)
		}
	}
	if EntityKind("bogus").Valid() {
		t.Error(`EntityKind("bogus").Valid() = true, want false`)
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Reference
	}{
		{"ABC-123", Reference{"ABC", KindTicket, 123}},
		{"#ABC-123", Reference{"ABC", KindTicket, 123}}, // '#'-prefixed form
		{"ABC-F12", Reference{"ABC", KindFeature, 12}},
		{"ABC-D7", Reference{"ABC", KindDecision, 7}},
		{"ABC-P4", Reference{"ABC", KindPlan, 4}},
		{"ABC-DOC9", Reference{"ABC", KindDocument, 9}}, // must not match the "D" branch
		{"TIX9-1", Reference{"TIX9", KindTicket, 1}},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := Parse(c.in)
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("Parse(%q) = %+v, want %+v", c.in, got, c.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	invalid := []string{
		"abc-123",  // lowercase project key
		"ABC-0",    // sequence must be positive
		"ABC-007",  // no leading zeros
		"ABC-X123", // unknown kind code
		"ABC123",   // missing hyphen
		"ABC-",     // missing sequence
		"",
		"#",
		"ABC--123",
	}
	for _, s := range invalid {
		t.Run(s, func(t *testing.T) {
			if _, err := Parse(s); err == nil {
				t.Errorf("Parse(%q) = nil error, want error", s)
			}
		})
	}
}

func TestFormatParseRoundTrip(t *testing.T) {
	kinds := []EntityKind{KindTicket, KindFeature, KindDecision, KindPlan, KindDocument}
	for _, k := range kinds {
		ref := Reference{ProjectKey: "ABC", Kind: k, Seq: 42}
		s, err := Format(ref)
		if err != nil {
			t.Fatalf("Format(%+v): %v", ref, err)
		}
		got, err := Parse(s)
		if err != nil {
			t.Fatalf("Parse(%q): %v", s, err)
		}
		if got != ref {
			t.Errorf("round trip: Format(%+v) -> %q -> Parse -> %+v", ref, s, got)
		}
	}
}
