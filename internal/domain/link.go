package domain

import "net/url"

// allowedLinkSchemes is the scheme allow-list ValidateLinkURL enforces
// for a named external link (product spec §5.11, §10's sanitization
// requirement). http/https cover the ordinary case; mailto is a
// legitimate, commonly-linked non-http scheme with no script-execution
// risk. Anything else — in particular javascript: and data:, the two
// classic stored-XSS vectors for a "paste a URL, we render it as a
// clickable link" feature — is rejected.
var allowedLinkSchemes = map[string]bool{
	"http":   true,
	"https":  true,
	"mailto": true,
}

// ValidateLinkURL reports whether raw is a well-formed URL using an
// allowed scheme. It does not otherwise restrict the URL's shape (no
// host allow-list, no path validation) — only the scheme is a security
// boundary here; everything else is just an opaque string the server
// stores and a client renders as a link's href.
func ValidateLinkURL(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return allowedLinkSchemes[u.Scheme]
}
