package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

func mustCreateAdmin(t *testing.T, s *Service) {
	t.Helper()
	if _, err := s.CreateAdminAccount(context.Background(), "admin", "correct horse battery staple"); err != nil {
		t.Fatalf("CreateAdminAccount: %v", err)
	}
}

// TestCreateHumanAccountSecondAccount is Phase 7's core gap-closing
// test: through Phase 6 there was no way to create a second human
// account at all (docs/troubleshooting.md), only the one-time
// CreateAdminAccount bootstrap.
func TestCreateHumanAccountSecondAccount(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateAdmin(t, s)
	adminActor := domain.ActorRef{Kind: domain.ActorHuman, Name: "admin"}

	ref, err := s.CreateHumanAccount(ctx, CreateHumanAccountRequest{
		Username: "bob", Password: "another-strong-password",
	}, adminActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateHumanAccount: %v", err)
	}
	if ref.Kind != domain.ActorHuman || ref.Name != "bob" {
		t.Errorf("ref = %+v, want human:bob", ref)
	}

	// It can authenticate immediately.
	_, ok, err := s.Authenticate(ctx, "bob", "another-strong-password", "127.0.0.1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !ok {
		t.Error("Authenticate(bob) = false, want true — the new account should be able to log in")
	}

	// Duplicate username is rejected.
	if _, err := s.CreateHumanAccount(ctx, CreateHumanAccountRequest{
		Username: "bob", Password: "yet-another-password",
	}, adminActor, testCorrelationID); err == nil {
		t.Error("CreateHumanAccount with a duplicate username: want an error, got nil")
	} else {
		var svcErr *Error
		if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrAlreadyExists {
			t.Errorf("duplicate username error = %v, want already_exists", err)
		}
	}
}

// TestCreateHumanAccountAttachesPasswordToImportedActor covers the
// gap docs/backup-recovery.md used to describe as open: `tickets
// import` creates a human actor row but never a human_accounts row
// (passwords are never exported), so that actor previously had no way
// to ever log in. CreateHumanAccount now detects this case (an actor
// exists, but no account row does) and attaches a password to the
// existing actor instead of trying to create a new one.
func TestCreateHumanAccountAttachesPasswordToImportedActor(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateAdmin(t, s)
	adminActor := domain.ActorRef{Kind: domain.ActorHuman, Name: "admin"}

	// Simulate what `tickets import` leaves behind: a human actor row
	// with no matching human_accounts row.
	if _, err := store.CreateActor(ctx, s.store.DB(), domain.ActorHuman, "imported-alice", "", nil, store.Now()); err != nil {
		t.Fatalf("seed imported actor: %v", err)
	}

	ref, err := s.CreateHumanAccount(ctx, CreateHumanAccountRequest{
		Username: "imported-alice", Password: "alices-new-password",
	}, adminActor, testCorrelationID)
	if err != nil {
		t.Fatalf("CreateHumanAccount for imported actor: %v", err)
	}
	if ref.Kind != domain.ActorHuman || ref.Name != "imported-alice" {
		t.Errorf("ref = %+v, want human:imported-alice", ref)
	}

	_, ok, err := s.Authenticate(ctx, "imported-alice", "alices-new-password", "127.0.0.1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !ok {
		t.Error("Authenticate(imported-alice) = false, want true — the attached password should work")
	}

	// A second attempt is now a normal already_exists — the account
	// row exists this time, not just the actor.
	if _, err := s.CreateHumanAccount(ctx, CreateHumanAccountRequest{
		Username: "imported-alice", Password: "irrelevant",
	}, adminActor, testCorrelationID); err == nil {
		t.Error("CreateHumanAccount for an already-attached actor: want an error, got nil")
	} else {
		var svcErr *Error
		if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrAlreadyExists {
			t.Errorf("second attach attempt error = %v, want already_exists", err)
		}
	}
}

func TestCreateHumanAccountRequiresUsernameAndPassword(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateAdmin(t, s)
	adminActor := domain.ActorRef{Kind: domain.ActorHuman, Name: "admin"}

	if _, err := s.CreateHumanAccount(ctx, CreateHumanAccountRequest{Password: "x"}, adminActor, testCorrelationID); err == nil {
		t.Error("CreateHumanAccount with no username: want an error, got nil")
	}
	if _, err := s.CreateHumanAccount(ctx, CreateHumanAccountRequest{Username: "carol"}, adminActor, testCorrelationID); err == nil {
		t.Error("CreateHumanAccount with no password: want an error, got nil")
	}
}

// TestChangePasswordSelfServiceRequiresOldPassword confirms a human
// changing their own password must supply and pass the current one,
// and that the new password actually takes effect.
func TestChangePasswordSelfServiceRequiresOldPassword(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateAdmin(t, s)
	adminActor := domain.ActorRef{Kind: domain.ActorHuman, Name: "admin"}

	// Wrong old password is rejected.
	if err := s.ChangePassword(ctx, ChangePasswordRequest{
		Username: "admin", OldPassword: "wrong", NewPassword: "new-password-here", SelfService: true,
	}, adminActor, testCorrelationID); err == nil {
		t.Error("ChangePassword with wrong old_password: want an error, got nil")
	}

	// No old password at all is rejected before ever touching the store.
	if err := s.ChangePassword(ctx, ChangePasswordRequest{
		Username: "admin", NewPassword: "new-password-here", SelfService: true,
	}, adminActor, testCorrelationID); err == nil {
		t.Error("ChangePassword self-service with no old_password: want an error, got nil")
	}

	// Correct old password succeeds and the new one actually works.
	if err := s.ChangePassword(ctx, ChangePasswordRequest{
		Username: "admin", OldPassword: "correct horse battery staple", NewPassword: "new-password-here", SelfService: true,
	}, adminActor, testCorrelationID); err != nil {
		t.Fatalf("ChangePassword with correct old_password: %v", err)
	}
	if _, ok, err := s.Authenticate(ctx, "admin", "new-password-here", "127.0.0.1"); err != nil || !ok {
		t.Errorf("Authenticate with new password = ok=%v err=%v, want ok=true", ok, err)
	}
	if _, ok, err := s.Authenticate(ctx, "admin", "correct horse battery staple", "127.0.0.1"); err != nil || ok {
		t.Errorf("Authenticate with old password after change = ok=%v err=%v, want ok=false", ok, err)
	}
}

// TestChangePasswordAdminResetSkipsOldPassword confirms an admin can
// reset a different account's password without ever supplying the
// current one.
func TestChangePasswordAdminResetSkipsOldPassword(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateAdmin(t, s)
	adminActor := domain.ActorRef{Kind: domain.ActorHuman, Name: "admin"}

	if _, err := s.CreateHumanAccount(ctx, CreateHumanAccountRequest{
		Username: "bob", Password: "bobs-original-password",
	}, adminActor, testCorrelationID); err != nil {
		t.Fatalf("CreateHumanAccount: %v", err)
	}

	if err := s.ChangePassword(ctx, ChangePasswordRequest{
		Username: "bob", NewPassword: "admin-reset-password", SelfService: false,
	}, adminActor, testCorrelationID); err != nil {
		t.Fatalf("ChangePassword (admin reset): %v", err)
	}
	if _, ok, err := s.Authenticate(ctx, "bob", "admin-reset-password", "127.0.0.1"); err != nil || !ok {
		t.Errorf("Authenticate with reset password = ok=%v err=%v, want ok=true", ok, err)
	}
}

// TestChangePasswordInvalidatesExistingSessions confirms a changed
// password ends every session for that account immediately, rather
// than leaving already-issued cookies valid until their own expiry.
func TestChangePasswordInvalidatesExistingSessions(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateAdmin(t, s)
	adminActor := domain.ActorRef{Kind: domain.ActorHuman, Name: "admin"}

	actorID, err := store.GetActorIDByRef(ctx, s.store.DB(), domain.ActorHuman, "admin")
	if err != nil {
		t.Fatalf("resolve actor id: %v", err)
	}
	if err := store.CreateSession(ctx, s.store.DB(), "session-1", actorID, "csrf-1", "2999-01-01T00:00:00.000000000Z", store.Now()); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := store.GetSession(ctx, s.store.DB(), "session-1"); err != nil {
		t.Fatalf("GetSession before password change: %v", err)
	}

	if err := s.ChangePassword(ctx, ChangePasswordRequest{
		Username: "admin", OldPassword: "correct horse battery staple", NewPassword: "new-password-here", SelfService: true,
	}, adminActor, testCorrelationID); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	if _, err := store.GetSession(ctx, s.store.DB(), "session-1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetSession after password change = %v, want ErrNotFound (session should be invalidated)", err)
	}
}

// TestPasswordChangeAuditEventNeverCarriesPasswordValue mirrors
// TestAgentTokenAuditEventNeverCarriesTokenValue's contract for the
// new account_created/password_changed audit events.
func TestPasswordChangeAuditEventNeverCarriesPasswordValue(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateAdmin(t, s)
	adminActor := domain.ActorRef{Kind: domain.ActorHuman, Name: "admin"}

	if _, err := s.CreateHumanAccount(ctx, CreateHumanAccountRequest{
		Username: "bob", Password: "bobs-secret-password",
	}, adminActor, testCorrelationID); err != nil {
		t.Fatalf("CreateHumanAccount: %v", err)
	}
	if err := s.ChangePassword(ctx, ChangePasswordRequest{
		Username: "bob", NewPassword: "bobs-new-secret-password", SelfService: false,
	}, adminActor, testCorrelationID); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	bobID, err := store.GetActorIDByRef(ctx, s.store.DB(), domain.ActorHuman, "bob")
	if err != nil {
		t.Fatalf("resolve bob actor id: %v", err)
	}
	events, err := store.ListActorAuditEvents(ctx, s.store.DB(), bobID)
	if err != nil {
		t.Fatalf("ListActorAuditEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no audit events recorded for bob")
	}
	for _, e := range events {
		for _, secret := range []string{"bobs-secret-password", "bobs-new-secret-password"} {
			if strings.Contains(e.Changes, secret) {
				t.Fatalf("audit event %q changes contains a plaintext password: %s", e.EventType, e.Changes)
			}
		}
	}
}

func TestListHumanAccounts(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	mustCreateAdmin(t, s)
	adminActor := domain.ActorRef{Kind: domain.ActorHuman, Name: "admin"}

	if _, err := s.CreateHumanAccount(ctx, CreateHumanAccountRequest{
		Username: "bob", Password: "bobs-password",
	}, adminActor, testCorrelationID); err != nil {
		t.Fatalf("CreateHumanAccount: %v", err)
	}

	accounts, err := s.ListHumanAccounts(ctx)
	if err != nil {
		t.Fatalf("ListHumanAccounts: %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("ListHumanAccounts = %d accounts, want 2 (admin + bob)", len(accounts))
	}
	var sawAdmin, sawBob bool
	for _, a := range accounts {
		if a.Username == "admin" && a.IsAdmin {
			sawAdmin = true
		}
		if a.Username == "bob" && !a.IsAdmin {
			sawBob = true
		}
	}
	if !sawAdmin || !sawBob {
		t.Errorf("ListHumanAccounts = %+v, want admin (is_admin=true) and bob (is_admin=false)", accounts)
	}
}
