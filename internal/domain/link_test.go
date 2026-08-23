package domain

import "testing"

func TestValidateLinkURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://example.com/design", true},
		{"http://example.com", true},
		{"mailto:someone@example.com", true},
		{"javascript:alert(1)", false},
		{"data:text/html,<script>alert(1)</script>", false},
		{"file:///etc/passwd", false},
		{"not-a-url", false},
		{"", false},
		{"//example.com/no-scheme", false},
	}
	for _, c := range cases {
		if got := ValidateLinkURL(c.url); got != c.want {
			t.Errorf("ValidateLinkURL(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}
