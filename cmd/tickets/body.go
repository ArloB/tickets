package main

import (
	"fmt"
	"io"
	"os"
)

// readBodyFile reads Markdown body content from a file path, or from
// stdin when path is "-" — product spec §7.3's "Markdown bodies
// accepted as a flag, from a file, or from stdin," the file/stdin half
// (a plain inline flag covers the third). Shared by every command that
// takes a Markdown body from a *-file flag (ticket update's
// --description-file today; comment/decision commands later).
func readBodyFile(path string) (string, error) {
	if path == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read body from stdin: %w", err)
		}
		return string(b), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read body file %s: %w", path, err)
	}
	return string(b), nil
}
