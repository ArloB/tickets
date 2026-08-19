// Package config resolves server and CLI configuration from flags,
// environment variables, and an OS-appropriate config file, in that
// documented precedence order (product spec §7.3).
//
// Implemented starting Phase 0 Step 5: the server's --data-dir flag and
// the 127.0.0.1-by-default bind address. The full precedence chain and
// remaining server settings land in Phase 2.
package config
