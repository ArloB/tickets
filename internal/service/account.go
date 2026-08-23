package service

import (
	"context"
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
