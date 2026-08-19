// Package mcpsrv registers the MCP tool surface once and serves it over
// both the Streamable HTTP handler and the stdio bridge, calling
// internal/service directly so no business logic is duplicated between
// transports (product spec §7.1, §8.1).
package mcpsrv
