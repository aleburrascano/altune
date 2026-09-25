package redact

import "regexp"

const Mask = "REDACTED"

// secretParamRe matches a known secret query param and its value anywhere a URL
// appears (a bare URL or one embedded in an error string). The param name and
// the leading delimiter are captured so only the value is dropped.
var secretParamRe = regexp.MustCompile(
	`(?i)([?&](?:api_key|apikey|access_token|client_secret|token|secret|password|pwd|key|auth|totp|totpserver|client_id)=)[^&\s"'\\]*`,
)

// Secrets masks the values of known secret params in any URL, query, or form
// text found in s, keeping host, path, and non-secret params intact for
// diagnostics. It is safe on plain URLs, on error strings that embed a URL, and
// on a body that is not URL-shaped at all, and it is idempotent.
func Secrets(s string) string {
	s = secretParamRe.ReplaceAllString(s, "${1}"+Mask)
	return maskedKeyedFields(maskedKeyedParams(s))
}

// keyedFieldRe matches a `"name": "value"` pair in text that is not a decodable
// JSON document — a body truncated by a read error, JSON embedded in a script
// or an error string, newline-delimited JSON — so a credential does not ride
// out on a document the structural walk could not parse.
var keyedFieldRe = regexp.MustCompile(`(?i)"([a-z0-9_.\-]{1,64})"(\s*:\s*)"(?:[^"\\]|\\.)*"`)

func maskedKeyedFields(s string) string {
	return keyedFieldRe.ReplaceAllStringFunc(s, func(field string) string {
		m := keyedFieldRe.FindStringSubmatch(field)
		if !IsSecretKey(m[1]) {
			return field
		}
		return `"` + m[1] + `"` + m[2] + `"` + Mask + `"`
	})
}

// keyedParamRe matches a "name=value" pair in query or form-encoded text, with
// the name captured so IsSecretKey decides. It reaches the credential names the
// fixed lists above do not spell out (refresh_token, accessToken) and, anchored
// at ^, the first pair of a form body that has no leading delimiter.
var keyedParamRe = regexp.MustCompile(`(?i)(^|[?&])([a-z0-9_.\[\]-]{1,64})=[^&\s"'\\]*`)

func maskedKeyedParams(s string) string {
	return keyedParamRe.ReplaceAllStringFunc(s, func(pair string) string {
		m := keyedParamRe.FindStringSubmatch(pair)
		if !IsSecretKey(m[2]) {
			return pair
		}
		return m[1] + m[2] + "=" + Mask
	})
}
