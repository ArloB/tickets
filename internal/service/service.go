package service

import (
	"context"

	"github.com/ArloB/tickets/internal/store"
)

// Service is the single authorization/validation/transaction/
// idempotency boundary shared by internal/httpapi and internal/mcpsrv
// (package doc.go, ADR 0005/0006). Neither caller talks to
// internal/store directly.
type Service struct {
	store *store.Store
}

func New(s *store.Store) *Service {
	return &Service{store: s}
}

// Ping confirms the database is reachable, for /readyz (product spec
// §9).
func (s *Service) Ping(ctx context.Context) error {
	return s.store.Ping(ctx)
}
