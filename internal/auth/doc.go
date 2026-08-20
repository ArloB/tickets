// Package auth implements Phase 2's identity primitives: Argon2id
// password hashing, bearer/session token generation and hashing, the
// Principal type carrying a request's resolved actor/permission/admin
// state through context, and the DB-persisted login-attempt throttle.
// See ADR 0004 for the design this implements and product spec §10 for
// the security requirements it satisfies.
package auth
