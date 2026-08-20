package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ArloB/tickets/internal/domain"
)

func TestCreateSession(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	sessionID, csrfToken, expiresAt, err := s.CreateSession(ctx, testActor)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sessionID == "" || csrfToken == "" {
		t.Fatalf("CreateSession returned empty id/csrf: %q %q", sessionID, csrfToken)
	}
	if sessionID == csrfToken {
		t.Error("session id equals its own csrf token")
	}
	if !expiresAt.After(time.Now()) {
		t.Errorf("CreateSession expiresAt = %v, want a future time", expiresAt)
	}
}

func TestCreateSessionDistinctIDsPerCall(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	id1, _, _, err := s.CreateSession(ctx, testActor)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	id2, _, _, err := s.CreateSession(ctx, testActor)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if id1 == id2 {
		t.Error("two sessions for the same actor got the same id")
	}
}

func TestResolveSession(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	admin, err := s.CreateAdminAccount(ctx, "admin", "hunter22")
	if err != nil {
		t.Fatalf("CreateAdminAccount: %v", err)
	}

	sessionID, csrfToken, _, err := s.CreateSession(ctx, admin)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	info, err := s.ResolveSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("ResolveSession: %v", err)
	}
	if info.Actor != admin || !info.IsAdmin || info.CSRFToken != csrfToken {
		t.Errorf("ResolveSession = %+v, want actor=%v admin=true csrf=%s", info, admin, csrfToken)
	}
}

func TestResolveSessionRejectsUnknownID(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	_, err := s.ResolveSession(ctx, "not-a-real-session")
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrUnauthorized {
		t.Fatalf("ResolveSession(unknown) error = %v, want unauthorized", err)
	}
}

func TestResolveSessionRejectsExpiredSession(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	admin, err := s.CreateAdminAccount(ctx, "admin", "hunter22")
	if err != nil {
		t.Fatalf("CreateAdminAccount: %v", err)
	}
	sessionID, _, _, err := s.CreateSession(ctx, admin)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Manipulate expires_at directly rather than sleeping (product spec
	// §15's recovery-test convention applies equally well to a plain
	// unit test — no real clock skew needed to prove the check works).
	if _, err := s.store.DB().Exec(
		`UPDATE sessions SET expires_at = ? WHERE id = ?`, "2020-01-01T00:00:00.000000000Z", sessionID,
	); err != nil {
		t.Fatalf("backdate session: %v", err)
	}

	_, err = s.ResolveSession(ctx, sessionID)
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrUnauthorized {
		t.Fatalf("ResolveSession(expired) error = %v, want unauthorized", err)
	}
}

func TestDeleteSessionInvalidatesIt(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	admin, err := s.CreateAdminAccount(ctx, "admin", "hunter22")
	if err != nil {
		t.Fatalf("CreateAdminAccount: %v", err)
	}
	sessionID, _, _, err := s.CreateSession(ctx, admin)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := s.DeleteSession(ctx, sessionID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := s.ResolveSession(ctx, sessionID); err == nil {
		t.Error("ResolveSession after DeleteSession: want error, got nil")
	}
}

func TestDeleteSessionOfUnknownIDIsNotAnError(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if err := s.DeleteSession(ctx, "never-existed"); err != nil {
		t.Errorf("DeleteSession(never-existed) = %v, want nil (logout is idempotent)", err)
	}
}
