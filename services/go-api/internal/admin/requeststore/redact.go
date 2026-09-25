package requeststore

import (
	"altune/go-api/internal/shared/redact"
	"regexp"
)

// secretFieldRe matches the value of a known credential field in a captured
// response body: the token/session fields returned by the Spotify access-token
// and client-token flows and Amazon Music's config.json / headers bundle. The
// key may sit at any JSON escaping depth (Amazon nests an escaped JSON bundle)
// and the value may be cut off by body truncation, so this is a lexical match,
// not a JSON parse. The key and opening quote are captured so only the value
// is dropped.
var secretFieldRe = regexp.MustCompile(
	`(?i)(\\*"(?:access_?token|refresh_?token|id_?token|client_?token|dev(?:eloper)?_?token|api_?token|csrf_?token|auth_?token|token|session_?id|api_?key|access_?key|client_?secret|secret|password|authorization|x-amzn-authentication|x-amzn-session-id|x-amzn-csrf)\\*"\s*:\s*\\*")(?:[^"\\]|\\[^"\\])*`,
)

// jwtRe matches a JWT anywhere in a body, including one cut off by truncation.
// Apple Music's anonymous devToken sits in a JS bundle under an unquoted key.
var jwtRe = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]*(?:\.[A-Za-z0-9_-]*)?`)

// cutOffJWTRe matches a JWT the body cap cut before its second segment, which
// jwtRe requires. It is anchored at the end because only the last bytes of a
// capture are truncated: elsewhere an eyJ-prefixed value is a complete one.
var cutOffJWTRe = regexp.MustCompile(`eyJ[A-Za-z0-9_-]*\.?[A-Za-z0-9_-]*$`)

// RedactBody masks credential values in a captured response body before it is
// stored. The shared redactor owns the credential vocabulary, so a name learned
// there is masked here; the regexes then reach what its structural walk cannot,
// a key nested inside an escaped JSON string and a value truncation cut off
// mid-token. Every other byte is kept so the capture stays useful for result
// debugging.
func RedactBody(s string) string {
	s = redact.SecretsInBody(s)
	s = secretFieldRe.ReplaceAllString(s, "${1}"+redact.Mask)
	s = jwtRe.ReplaceAllString(s, redact.Mask)
	return cutOffJWTRe.ReplaceAllString(s, redact.Mask)
}
