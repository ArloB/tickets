package service

import (
	"context"
	"database/sql"
	"fmt"

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

// txFunc is a mutation's body, run inside a managed transaction. now is
// a single timestamp shared by every row the function writes (see
// store.Now) — rows created or updated together by one logical
// mutation get an identical created_at/updated_at rather than drifting
// by microseconds, which matters once audit events need to agree with
// the rows they describe. Any error returned rolls the transaction
// back; a nil return commits it.
type txFunc func(tx *sql.Tx, now string) error

// withTx owns the BeginTx/commit/rollback boilerplate that used to be
// hand-repeated in every mutating service method (createProjectTx,
// createTicketTx, UpdateTicketStatus). Centralizing it here is what
// lets Phase 1 add audit-event emission in one place instead of
// re-deriving the same deferred-rollback pattern at every call site.
//
// BEGIN IMMEDIATE (via the store DSN's _txlock=immediate, ADR 0003) is
// what fn's transaction actually issues — this helper doesn't touch
// isolation, only lifecycle.
func (s *Service) withTx(ctx context.Context, fn txFunc) error {
	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("service: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	now := store.Now()
	if err := fn(tx, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("service: commit: %w", err)
	}
	committed = true
	return nil
}

// A read-only counterpart to withTx (&sql.TxOptions{ReadOnly: true})
// lands in Step 4 alongside its first real caller — a multi-statement
// read that needs one consistent snapshot (e.g. a ticket plus its
// relationships and comments). Until then, every read in this package
// is a single autocommit query and doesn't need it; see ADR 0003 for
// why a plain BeginTx(ctx, nil) would be wrong for that case (it
// silently takes the write lock via the store DSN's _txlock=immediate).
