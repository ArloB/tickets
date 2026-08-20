package service

import (
	"context"
	"errors"
	"testing"

	"github.com/ArloB/tickets/internal/domain"
)

func TestCreateAdminAccount(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	ref, err := s.CreateAdminAccount(ctx, "admin", "hunter22")
	if err != nil {
		t.Fatalf("CreateAdminAccount: %v", err)
	}
	if ref.Kind != domain.ActorHuman || ref.Name != "admin" {
		t.Errorf("CreateAdminAccount returned %v, want human:admin", ref)
	}

	actor, ok, err := s.Authenticate(ctx, "admin", "hunter22", "127.0.0.1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !ok {
		t.Fatalf("Authenticate with the just-created password: want success, got failure")
	}
	if actor != ref {
		t.Errorf("Authenticate returned %v, want %v", actor, ref)
	}
}

func TestCreateAdminAccountRefusesSecond(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateAdminAccount(ctx, "admin", "hunter22"); err != nil {
		t.Fatalf("first CreateAdminAccount: %v", err)
	}
	_, err := s.CreateAdminAccount(ctx, "someone-else", "another-password")
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrAlreadyExists {
		t.Fatalf("second CreateAdminAccount error = %v, want already_exists", err)
	}
}

func TestCreateAdminAccountValidation(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateAdminAccount(ctx, "", "hunter22"); err == nil {
		t.Error("CreateAdminAccount with empty username: want error, got nil")
	}
	if _, err := s.CreateAdminAccount(ctx, "admin", ""); err == nil {
		t.Error("CreateAdminAccount with empty password: want error, got nil")
	}
}

func TestAuthenticateRejectsWrongPassword(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateAdminAccount(ctx, "admin", "hunter22"); err != nil {
		t.Fatalf("CreateAdminAccount: %v", err)
	}

	_, ok, err := s.Authenticate(ctx, "admin", "wrong-password", "127.0.0.1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if ok {
		t.Error("Authenticate with a wrong password: want failure, got success")
	}
}

func TestAuthenticateRejectsUnknownUsername(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	_, ok, err := s.Authenticate(ctx, "nobody", "whatever", "127.0.0.1")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if ok {
		t.Error("Authenticate with an unknown username: want failure, got success")
	}
}

// TestAuthenticateThrottlesAfterMaxFailures confirms the service layer
// actually wires internal/auth's throttle in, not just that the
// throttle package works correctly in isolation.
func TestAuthenticateThrottlesAfterMaxFailures(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)

	if _, err := s.CreateAdminAccount(ctx, "admin", "hunter22"); err != nil {
		t.Fatalf("CreateAdminAccount: %v", err)
	}

	for i := 0; i < loginThrottleMax; i++ {
		if _, _, err := s.Authenticate(ctx, "admin", "wrong-password", "127.0.0.1"); err != nil {
			t.Fatalf("attempt %d: unexpected error before throttling should kick in: %v", i, err)
		}
	}

	_, _, err := s.Authenticate(ctx, "admin", "hunter22", "127.0.0.1")
	var svcErr *Error
	if !errors.As(err, &svcErr) || svcErr.Code != domain.ErrThrottled {
		t.Fatalf("Authenticate after %d failures error = %v, want throttled (even with the correct password)", loginThrottleMax, err)
	}
}
