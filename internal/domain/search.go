package domain

import "strings"

// SanitizeFTSQuery turns raw user input into a safe FTS5 MATCH query.
// FTS5's MATCH argument is a query *language*, not a literal — a bare
// colon, an unbalanced quote, a lone "AND"/"OR"/"NOT", or a trailing
// "*" is a syntax error SQLite raises as a query-time error, not a
// zero-result search. Wrapping every whitespace-separated term in
// double quotes (doubling any embedded quote, FTS5's own escape)
// turns the whole input into a sequence of phrase queries, which is
// syntactically safe for any input — phrase queries are implicitly
// ANDed together by FTS5's default query syntax, matching what a
// user typing a few keywords expects.
func SanitizeFTSQuery(raw string) string {
	fields := strings.Fields(raw)
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		terms = append(terms, `"`+strings.ReplaceAll(f, `"`, `""`)+`"`)
	}
	return strings.Join(terms, " ")
}
