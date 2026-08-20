package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/ArloB/tickets/internal/store"
)

// TooManyAttempts reports whether username or ip has at least max
// failed login attempts within the trailing window, backing a DB-
// persisted throttle (migration 0004's login_attempts comment explains
// why DB-persisted rather than in-memory: a restart must not hand an
// attacker a fresh window). Checked against both username and ip
// independently, so an attacker can't dodge a per-username lock by
// rotating source addresses, or dodge a per-IP lock by rotating
// usernames.
func TooManyAttempts(ctx context.Context, q store.Querier, username, ip string, window time.Duration, max int) (bool, error) {
	cutoff := time.Now().UTC().Add(-window).Format(store.TimeLayout)

	var byUsername int
	if err := q.QueryRowContext(ctx,
		`SELECT count(*) FROM login_attempts WHERE username = ? AND succeeded = 0 AND created_at >= ?`,
		username, cutoff,
	).Scan(&byUsername); err != nil {
		return false, fmt.Errorf("auth: count failed attempts by username: %w", err)
	}
	if byUsername >= max {
		return true, nil
	}

	var byIP int
	if err := q.QueryRowContext(ctx,
		`SELECT count(*) FROM login_attempts WHERE ip = ? AND succeeded = 0 AND created_at >= ?`,
		ip, cutoff,
	).Scan(&byIP); err != nil {
		return false, fmt.Errorf("auth: count failed attempts by ip: %w", err)
	}
	return byIP >= max, nil
}

// RecordAttempt logs one login attempt, success or failure. Every
// attempt is recorded - nothing prunes successes out, since the
// throttle above only ever counts failures in a trailing window (an
// unbounded intermixed log is simpler than pruning).
func RecordAttempt(ctx context.Context, q store.Querier, username, ip string, succeeded bool, now string) error {
	succ := 0
	if succeeded {
		succ = 1
	}
	if _, err := q.ExecContext(ctx,
		`INSERT INTO login_attempts(username, ip, succeeded, created_at) VALUES (?, ?, ?, ?)`,
		username, ip, succ, now,
	); err != nil {
		return fmt.Errorf("auth: record login attempt: %w", err)
	}
	return nil
}
