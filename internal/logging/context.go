package logging

import "context"

type correlationIDKey struct{}

// WithCorrelationID attaches a request's correlation id to ctx, so
// code further down a call chain can log it without needing the
// original *http.Request in scope. internal/httpapi's authentication
// middleware (Phase 2 auth work) is the first caller.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey{}, id)
}

// CorrelationIDFromContext returns the id WithCorrelationID attached,
// or "" if none was.
func CorrelationIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(correlationIDKey{}).(string)
	return id
}
