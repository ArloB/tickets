package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
	"github.com/google/uuid"
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

// NewCorrelationID generates a fresh correlation id for a caller that
// has no client-supplied one to echo (product spec §9's "client-
// generated correlation ID" is optional). internal/httpapi already has
// its own header-aware correlationID(r) — it prefers the client's
// X-Correlation-Id and falls back to this same UUIDv7 shape only when
// absent. internal/mcpsrv has no HTTP request to read a header from
// at all, so every tool call generates one via this function.
func NewCorrelationID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return "unknown"
	}
	return id.String()
}

// txFunc is a mutation's body, run inside a managed transaction.
// actorID is the internal id withTx already resolved from the caller-
// supplied domain.ActorRef (ADR 0012) — fn stamps it on every row it
// writes and on any audit_events row it emits via
// store.InsertAuditEvent, using correlationID for the same event. now
// is a single timestamp shared by every row the function writes (see
// store.Now) — rows created or updated together by one logical
// mutation get an identical created_at/updated_at, including the audit
// event describing them. Any error returned rolls the transaction
// back; a nil return commits it.
type txFunc func(tx *sql.Tx, actorID int64, correlationID string, now string) error

// withTx owns the BeginTx/actor-resolution/commit/rollback boilerplate
// that used to be hand-repeated in every mutating service method
// (createProjectTx, createTicketTx, UpdateTicketStatus). Centralizing
// it here is what makes audit-event emission structurally hard to
// forget rather than a per-method reminder — every mutation resolves
// the same way.
//
// BEGIN IMMEDIATE (via the store DSN's _txlock=immediate, ADR 0003) is
// what fn's transaction actually issues — this helper doesn't touch
// isolation, only lifecycle.
func (s *Service) withTx(ctx context.Context, actor domain.ActorRef, correlationID string, fn txFunc) error {
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

	actorID, err := store.GetActorIDByRef(ctx, tx, actor.Kind, actor.Name)
	if err != nil {
		return fmt.Errorf("service: resolve actor %s: %w", actor, err)
	}

	now := store.Now()
	if err := fn(tx, actorID, correlationID, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("service: commit: %w", err)
	}
	committed = true
	return nil
}

// A read-only counterpart to withTx (&sql.TxOptions{ReadOnly: true})
// lands alongside its first real caller — a multi-statement read that
// needs one consistent snapshot (e.g. a ticket plus its relationships
// and comments). Until then, every read in this package is a single
// autocommit query and doesn't need it; see ADR 0003 for why a plain
// BeginTx(ctx, nil) would be wrong for that case (it silently takes
// the write lock via the store DSN's _txlock=immediate).
