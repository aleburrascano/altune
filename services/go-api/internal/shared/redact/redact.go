// Package redact masks credentials in diagnostic text before it is stored or
// written anywhere. It lives in internal/shared so both feature packages
// (admin/requeststore) and shared infrastructure (httptrace) use one helper;
// internal/shared may not import feature packages.
package redact

import "regexp"

// secretParamRe matches a known secret query param and its value anywhere a URL
// appears (a bare URL or one embedded in an error string). The param name and
// the leading delimiter are captured so only the value is dropped.
var secretParamRe = regexp.MustCompile(
	`(?i)([?&](?:api_key|apikey|access_token|client_secret|token|secret|password|pwd|key|auth)=)[^&\s"'\\]*`,
)

// Secrets masks the values of known secret query params in any URL found in s,
// keeping host, path, and non-secret params intact for diagnostics. It is safe
// on plain URLs and on error strings that embed a URL.
func Secrets(s string) string {
	return secretParamRe.ReplaceAllString(s, "${1}REDACTED")
}
