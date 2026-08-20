package logging

import "testing"

func TestNewReturnsUsableLogger(t *testing.T) {
	for _, format := range []string{"console", "json", ""} {
		l := New(format)
		if l == nil {
			t.Fatalf("New(%q) = nil", format)
		}
		// Must not panic on a real log call.
		l.Info("logging smoke test", "format", format)
	}
}

func TestCorrelationIDRoundTrip(t *testing.T) {
	ctx := WithCorrelationID(t.Context(), "abc-123")
	if got := CorrelationIDFromContext(ctx); got != "abc-123" {
		t.Errorf("CorrelationIDFromContext = %q, want abc-123", got)
	}
	if got := CorrelationIDFromContext(t.Context()); got != "" {
		t.Errorf("CorrelationIDFromContext on a context with none = %q, want \"\"", got)
	}
}
