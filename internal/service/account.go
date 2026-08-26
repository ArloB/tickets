package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ArloB/tickets/internal/auth"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/store"
)

// CreateAdminAccount creates the very first human account, with the
// operational admin flag set (product spec §4.2). Used by `tickets
// setup` and by the unauthenticated POST /api/v1/setup HTTP endpoint;
// refuses if any human account already exists, so first-run setup
// only ever runs once per installation.
//
// This is a bootstrap operation with no calling actor to attribute it
// to — it doesn't go through withTx, unlike every other mutating
// service method, because there is by definition no authenticated
// caller yet.
//
// The existence check runs twice: once up front (cheap, avoids paying
// for an Argon2id hash when setup has obviously already run) and once
// again inside the write transaction immediately before the insert.
// The second check is the one that actually matters — SQLite
// serializes write transactions, so re-checking with the transaction
// handle after BeginTx closes the TOCTOU window the first check alone
// would leave open. That window is unreachable from a single local
// `tickets setup` invocation but is very reachable from two concurrent
// requests to the HTTP endpoint, which is exactly the scenario an
// unauthenticated admin-creation route must not race.
func (s *Service) CreateAdminAccount(ctx context.Context, username, password string) (domain.ActorRef, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return domain.ActorRef{}, newValidationError("username", "username is required")
	}
	if password == "" {
		return domain.ActorRef{}, newValidationError("password", "password is required")
	}

	count, err := store.CountHumanAccounts(ctx, s.store.DB())
	if err != nil {
		return domain.ActorRef{}, fmt.Errorf("service: count human accounts: %w", err)
	}
	if count > 0 {
		return domain.ActorRef{}, newAlreadyExistsError("", "a human account already exists; first-run setup only runs once")
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return domain.ActorRef{}, fmt.Errorf("service: hash password: %w", err)
	}

	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return domain.ActorRef{}, fmt.Errorf("service: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	count, err = store.CountHumanAccounts(ctx, tx)
	if err != nil {
		return domain.ActorRef{}, fmt.Errorf("service: count human accounts: %w", err)
	}
	if count > 0 {
		return domain.ActorRef{}, newAlreadyExistsError("", "a human account already exists; first-run setup only runs once")
	}

	now := store.Now()
	actorID, err := store.CreateActor(ctx, tx, domain.ActorHuman, username, "", nil, now)
	if err != nil {
		return domain.ActorRef{}, fmt.Errorf("service: create actor: %w", err)
	}
	if err := store.CreateHumanAccount(ctx, tx, actorID, username, hash, true, now); err != nil {
		return domain.ActorRef{}, fmt.Errorf("service: create human account: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.ActorRef{}, fmt.Errorf("service: commit: %w", err)
	}
	committed = true

	return domain.ActorRef{Kind: domain.ActorHuman, Name: username}, nil
}

// CreateHumanAccountRequest is CreateHumanAccount's input.
type CreateHumanAccountRequest struct {
	Username string
	Password string
	IsAdmin  bool
}

// CreateHumanAccount creates a second (or later) human account (Phase
// 7 — product spec §4.2/§13 name account management as an admin
// capability; through Phase 6 the only human account creation path
// was CreateAdminAccount's one-time first-run bootstrap). Unlike
// CreateAdminAccount, this goes through s.withTx: there is a real
// calling actor to attribute the creation to (an admin, enforced by
// internal/httpapi's routeAdmin — this method itself doesn't check the
// caller's own admin flag, matching agent.go's CreateAgent, which
// likewise trusts its caller's permission gate rather than
// re-verifying it in internal/service).
//
// This also covers the imported-actor case (docs/backup-recovery.md):
// `tickets import` creates a human actor row but never a human_accounts
// row (passwords are deliberately never exported), so that actor has
// no way to log in. Rather than a separate command, CreateHumanAccount
// detects an existing actor with no account row and attaches a fresh
// password to it instead of creating a new actor — "already exists"
// means the account exists, not merely the actor.
func (s *Service) CreateHumanAccount(ctx context.Context, req CreateHumanAccountRequest, actor domain.ActorRef, correlationID string) (domain.ActorRef, error) {
	username := strings.TrimSpace(req.Username)
	if username == "" {
		return domain.ActorRef{}, newValidationError("username", "username is required")
	}
	if req.Password == "" {
		return domain.ActorRef{}, newValidationError("password", "password is required")
	}

	// Cheap pre-check outside the transaction (avoids hashing a
	// password for a request that's certain to fail); the check
	// that actually prevents a race is repeated inside withTx below,
	// same two-layer pattern CreateAdminAccount documents — SQLite
	// serializes write transactions, so re-checking with the
	// transaction handle after BeginTx closes the TOCTOU window.
	if _, err := accountStateForUsername(ctx, s.store.DB(), username); err != nil {
		return domain.ActorRef{}, err
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return domain.ActorRef{}, fmt.Errorf("service: hash password: %w", err)
	}

	err = s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		existingActorID, err := accountStateForUsername(ctx, tx, username)
		if err != nil {
			return err
		}

		targetActorID := existingActorID
		if targetActorID == 0 {
			targetActorID, err = store.CreateActor(ctx, tx, domain.ActorHuman, username, "", nil, now)
			if err != nil {
				return fmt.Errorf("service: create actor: %w", err)
			}
		}
		if err := store.CreateHumanAccount(ctx, tx, targetActorID, username, hash, req.IsAdmin, now); err != nil {
			return fmt.Errorf("service: create human account: %w", err)
		}
		changes := auditChanges(map[string]any{"username": username, "is_admin": req.IsAdmin})
		return store.InsertActorAuditEvent(ctx, tx, targetActorID, actorID, eventAccountCreated, corrID, changes, now)
	})
	if err != nil {
		return domain.ActorRef{}, err
	}
	return domain.ActorRef{Kind: domain.ActorHuman, Name: username}, nil
}

// accountStateForUsername resolves username to an actor id it's safe
// to attach a new human_accounts row to: 0 if no actor named username
// exists yet (the ordinary new-account path), or that actor's id if
// one exists but has no account row yet (the imported-actor path). An
// *Error wrapping already_exists is returned if an account row already
// exists, and a plain error for any other lookup failure.
func accountStateForUsername(ctx context.Context, q store.Querier, username string) (int64, error) {
	actorID, err := store.GetActorIDByRef(ctx, q, domain.ActorHuman, username)
	if errors.Is(err, store.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("service: check existing actor: %w", err)
	}
	if _, err := store.GetHumanAccountByActorID(ctx, q, actorID); errors.Is(err, store.ErrNotFound) {
		return actorID, nil
	} else if err != nil {
		return 0, fmt.Errorf("service: check existing account: %w", err)
	}
	return 0, newAlreadyExistsError("username", "a human account named %q already exists", username)
}

// ChangePasswordRequest is ChangePassword's input. OldPassword is
// required and verified when a human changes their own password;
// leave it "" for an admin resetting a different account's password
// (internal/httpapi enforces that the caller is either the target
// account or an admin — see requireAdmin/requireEditor — this method
// only enforces OldPassword's correctness when the caller supplies
// one, matching that split).
type ChangePasswordRequest struct {
	Username    string
	OldPassword string
	NewPassword string
	// SelfService is true when the caller is changing their own
	// password (OldPassword required and verified) and false for an
	// admin resetting someone else's (OldPassword ignored).
	SelfService bool
}

// ChangePassword replaces a human account's password (Phase 7) and
// invalidates every existing session for that account —
// store.DeleteSessionsByActor — so an already-issued cookie stops
// working immediately rather than remaining valid until its own
// expiry. No password or hash is ever placed in the audit event's
// changes (mirrors TestAgentTokenAuditEventNeverCarriesTokenValue's
// contract for tokens).
func (s *Service) ChangePassword(ctx context.Context, req ChangePasswordRequest, actor domain.ActorRef, correlationID string) error {
	if req.NewPassword == "" {
		return newValidationError("new_password", "new_password is required")
	}
	account, err := store.GetHumanAccountByUsername(ctx, s.store.DB(), req.Username)
	if errors.Is(err, store.ErrNotFound) {
		return newNotFoundError("account %q not found", req.Username)
	}
	if err != nil {
		return fmt.Errorf("service: look up account: %w", err)
	}

	if req.SelfService {
		if req.OldPassword == "" {
			return newValidationError("old_password", "old_password is required to change your own password")
		}
		ok, verr := auth.VerifyPassword(account.PasswordHash, req.OldPassword)
		if verr != nil {
			return fmt.Errorf("service: verify old password: %w", verr)
		}
		if !ok {
			return &Error{Code: domain.ErrValidationFailed, Field: "old_password", Message: "old_password is incorrect"}
		}
	}

	newHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		return fmt.Errorf("service: hash new password: %w", err)
	}

	return s.withTx(ctx, actor, correlationID, func(tx *sql.Tx, actorID int64, corrID, now string) error {
		if err := store.UpdateHumanAccountPassword(ctx, tx, account.ActorID, newHash, now); err != nil {
			return fmt.Errorf("service: update password: %w", err)
		}
		if err := store.DeleteSessionsByActor(ctx, tx, account.ActorID); err != nil {
			return fmt.Errorf("service: invalidate sessions: %w", err)
		}
		changes := auditChanges(map[string]any{"username": req.Username})
		return store.InsertActorAuditEvent(ctx, tx, account.ActorID, actorID, eventPasswordChanged, corrID, changes, now)
	})
}

// HumanAccountSummary is ListHumanAccounts' per-row output.
type HumanAccountSummary struct {
	Username  string
	IsAdmin   bool
	CreatedAt time.Time
}

// ListHumanAccounts returns every human account (admin view, product
// spec §13). Unpaginated — see store.ListHumanAccounts' doc comment
// for why.
func (s *Service) ListHumanAccounts(ctx context.Context) ([]HumanAccountSummary, error) {
	rows, err := store.ListHumanAccounts(ctx, s.store.DB())
	if err != nil {
		return nil, fmt.Errorf("service: list human accounts: %w", err)
	}
	out := make([]HumanAccountSummary, len(rows))
	for i, r := range rows {
		createdAt, err := time.Parse(store.TimeLayout, r.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("service: parse account created_at: %w", err)
		}
		out[i] = HumanAccountSummary{Username: r.Username, IsAdmin: r.IsAdmin, CreatedAt: createdAt}
	}
	return out, nil
}

// loginThrottleWindow/loginThrottleMax bound how many failed login
// attempts a username or source IP may make before Authenticate starts
// refusing outright (§10's throttling requirement). Ten attempts in
// fifteen minutes is generous enough not to lock out a human who
// mistypes a password twice, while still cutting an online guessing
// attempt off quickly.
const (
	loginThrottleWindow = 15 * time.Minute
	loginThrottleMax    = 10
)

// dummyPasswordHash is compared against on every Authenticate call for
// a username that doesn't exist, so the time VerifyPassword takes is
// the same whether or not the username is real — without this, an
// early return for "no such user" would make login attempts against
// real vs. fake usernames measurably faster or slower, which is
// exactly the timing oracle a username-enumeration attack looks for.
//
// Lazy via sync.OnceValue rather than computed at package init: this
// package is imported by every binary (`tickets --help`, `tickets
// mcp`, every test binary in the repo), and an Argon2id pass plus its
// 64 MiB working set has a real cost that only Authenticate actually
// needs to pay.
var dummyPasswordHash = sync.OnceValue(mustDummyPasswordHash)

func mustDummyPasswordHash() string {
	h, err := auth.HashPassword("tickets-dummy-password-for-constant-time-verification")
	if err != nil {
		panic(fmt.Sprintf("service: generate dummy password hash: %v", err))
	}
	return h
}

// Authenticate verifies a username/password pair (ADR 0004), checking
// the login throttle first and recording the outcome either way. It
// always calls VerifyPassword — against dummyPasswordHash when the
// username doesn't exist — so a caller can't distinguish "no such
// user" from "wrong password" by timing.
func (s *Service) Authenticate(ctx context.Context, username, password, ip string) (domain.ActorRef, bool, error) {
	db := s.store.DB()

	throttled, err := auth.TooManyAttempts(ctx, db, username, ip, loginThrottleWindow, loginThrottleMax)
	if err != nil {
		return domain.ActorRef{}, false, fmt.Errorf("service: check login throttle: %w", err)
	}
	if throttled {
		return domain.ActorRef{}, false, &Error{Code: domain.ErrThrottled, Message: "too many failed login attempts; try again later"}
	}

	account, err := store.GetHumanAccountByUsername(ctx, db, username)
	found := err == nil
	hash := dummyPasswordHash()
	if found {
		hash = account.PasswordHash
	}

	ok, verr := auth.VerifyPassword(hash, password)
	if verr != nil {
		return domain.ActorRef{}, false, fmt.Errorf("service: verify password: %w", verr)
	}
	succeeded := found && ok

	if err := auth.RecordAttempt(ctx, db, username, ip, succeeded, store.Now()); err != nil {
		return domain.ActorRef{}, false, fmt.Errorf("service: record login attempt: %w", err)
	}

	if !succeeded {
		return domain.ActorRef{}, false, nil
	}
	return domain.ActorRef{Kind: domain.ActorHuman, Name: username}, true, nil
}
