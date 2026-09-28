package redact

import "regexp"

const Mask = "REDACTED"

var secretParamRe = regexp.MustCompile(
	`(?i)([?&](?:api_key|apikey|access_token|client_secret|token|secret|password|pwd|key|auth|totp|totpserver|client_id)=)[^&\s"'\\]*`,
)

func Secrets(s string) string {
	s = secretParamRe.ReplaceAllString(s, "${1}"+Mask)
	return maskedKeyedFields(maskedKeyedParams(s))
}

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
