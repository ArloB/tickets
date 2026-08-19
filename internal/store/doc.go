// Package store owns the SQLite connection (pragmas: WAL, foreign_keys,
// busy_timeout), embedded schema migrations, and typed query access. No
// other package opens the database directly; this is the sole storage
// boundary (product spec §8.3).
package store
