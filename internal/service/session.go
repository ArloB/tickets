package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ArloB/tickets/internal/auth"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// sessionTTL bounds how long a session stays valid without the human
// logging in again (product spec §10: "session expiry"). 24 hours
// balances not forcing a re-login every day against not leaving a
// forgotten browser tab authenticated indefinitely.
const sessionTTL = 24 * time.Hour

// CreateSession issues a new session for actor (typically the ref
// Authenticate just returned): a fresh session id — the cookie's raw
// value — and an independent CSRF token, both from
// internal/auth.GenerateToken. See migration 0004's comment on
// sessions.id for why the session id itself is stored raw, unlike an
// agent bearer token's hash.
func (s *Service) CreateSession(ctx context.Context, actor domain.ActorRef) (sessionID, csrfToken string, expiresAt time.Time, err error) {
	actorID, err := store.GetActorIDByRef(ctx, s.store.DB(), actor.Kind, actor.Name)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("service: resolve actor %s: %w", actor, err)
	}

	rawID, _, err := auth.GenerateToken()
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("service: generate session id: %w", err)
	}
	rawCSRF, _, err := auth.GenerateToken()
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("service: generate csrf token: %w", err)
	}

	now := time.Now().UTC()
	expiresAt = now.Add(sessionTTL)
	if err := store.CreateSession(ctx, s.store.DB(), rawID, actorID, rawCSRF,
		expiresAt.Format(store.TimeLayout), now.Format(store.TimeLayout)); err != nil {
		return "", "", time.Time{}, fmt.Errorf("service: create session: %w", err)
	}
	return rawID, rawCSRF, expiresAt, nil
}

// SessionInfo is everything internal/httpapi's authentication
// middleware needs to build a Principal from a session cookie, without
// httpapi touching internal/store directly (ADR 0005: internal/service
// is the sole boundary shared by every transport).
type SessionInfo struct {
	Actor     domain.ActorRef
	IsAdmin   bool
	CSRFToken string
}

// ResolveSession resolves a session's raw cookie value to SessionInfo,
// or an unauthorized *Error if the session doesn't exist or has
// expired. A successful resolve also touches last_seen_at
// (store.TouchSession) as a side effect.
func (s *Service) ResolveSession(ctx context.Context, sessionID string) (SessionInfo, error) {
	db := s.store.DB()

	row, err := store.GetSession(ctx, db, sessionID)
	if errors.Is(err, store.ErrNotFound) {
		return SessionInfo{}, &Error{Code: domain.ErrUnauthorized, Message: "invalid session"}
	}
	if err != nil {
		return SessionInfo{}, fmt.Errorf("service: get session: %w", err)
	}

	expiresAt, perr := time.Parse(store.TimeLayout, row.ExpiresAt)
	if perr != nil {
		return SessionInfo{}, fmt.Errorf("service: parse session expiry: %w", perr)
	}
	if time.Now().UTC().After(expiresAt) {
		return SessionInfo{}, &Error{Code: domain.ErrUnauthorized, Message: "session has expired"}
	}

	actor, err := store.GetActorRefByID(ctx, db, row.ActorID)
	if err != nil {
		return SessionInfo{}, fmt.Errorf("service: resolve session actor: %w", err)
	}
	account, err := store.GetHumanAccountByActorID(ctx, db, row.ActorID)
	if err != nil {
		return SessionInfo{}, fmt.Errorf("service: resolve session account: %w", err)
	}

	if err := store.TouchSession(ctx, db, sessionID, store.Now()); err != nil {
		return SessionInfo{}, fmt.Errorf("service: touch session: %w", err)
	}

	return SessionInfo{Actor: actor, IsAdmin: account.IsAdmin, CSRFToken: row.CSRFToken}, nil
}

// DeleteSession removes a session (logout). A no-op, not an error, if
// the session doesn't exist — logging out with an already-gone or
// never-valid cookie shouldn't itself fail.
func (s *Service) DeleteSession(ctx context.Context, sessionID string) error {
	if err := store.DeleteSession(ctx, s.store.DB(), sessionID); err != nil {
		return fmt.Errorf("service: delete session: %w", err)
	}
	return nil
}
