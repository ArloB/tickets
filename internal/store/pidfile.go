package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

const pidfileName = "tickets.pid"

// WritePidfile records the running server's PID under dataDir —
// admin restore's precondition (Phase 6 Step 4, product spec §12)
// that the server must not be running while a restore swaps the
// database file in place. This is presence, not a liveness probe: a
// running-but-idle server holds no SQLite write lock for a lock-based
// check to catch, and there is no portable (Linux + Windows, ADR
// 0003's pure-Go constraint) way to ask "is PID N still alive" without
// platform-specific code. A stale pidfile left behind by a crash is a
// known, documented limitation of this approach — `admin restore
// --force` exists for that case; see docs/backup-recovery.md.
func WritePidfile(dataDir string) error {
	path := filepath.Join(dataDir, pidfileName)
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return fmt.Errorf("store: write pidfile: %w", err)
	}
	return nil
}

// RemovePidfile clears the pidfile WritePidfile wrote, on clean
// shutdown. A missing file is not an error — RemovePidfile may run
// after WritePidfile was never reached, or be called more than once.
func RemovePidfile(dataDir string) error {
	path := filepath.Join(dataDir, pidfileName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("store: remove pidfile: %w", err)
	}
	return nil
}

// PidfileExists reports whether dataDir currently has a pidfile —
// admin restore's "is the server running" check. See WritePidfile's
// doc comment for why this is presence, not a liveness probe.
func PidfileExists(dataDir string) (bool, error) {
	path := filepath.Join(dataDir, pidfileName)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("store: stat pidfile: %w", err)
	}
	return true, nil
}
