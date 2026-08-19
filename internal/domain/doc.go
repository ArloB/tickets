// Package domain holds pure, I/O-free domain logic: entity kind and
// workflow/priority/severity enums, public-reference parsing and
// formatting (ABC-123, ABC-F12, ABC-D7, ABC-P4, ABC-DOC9), and
// validation rules. Nothing here talks to the database or the network;
// see docs/contracts for the specifications this package implements.
package domain
