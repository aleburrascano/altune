package requeststore

import "regexp"

// secretParamRe matches a known secret query param and its value anywhere a URL
// appears (a bare URL or one embedded in an error string). The param name and
// the leading delimiter are captured so only the value is dropped.
var secretParamRe = regexp.MustCompile(
	`(?i)([?&](?:api_key|apikey|access_token|client_secret|token|secret|password|pwd|key|auth)=)[^&\s"'\\]*`,
)

// RedactSecrets masks the values of known secret query params in any URL found
// in s, keeping host, path, and non-secret params intact for diagnostics. It is
// safe on plain URLs and on error strings that embed a URL.
func RedactSecrets(s string) string {
	return secretParamRe.ReplaceAllString(s, "${1}REDACTED")
}
