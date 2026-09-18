package redact

import "strings"

// credentialKeyMarkers is the vocabulary of secret-bearing names this codebase
// actually handles: every Config secret field (tokens, API keys, access and
// secret keys) plus the credential fields providers answer with. A name is a
// credential when its lower-cased form contains any of these markers. Markers
// are deliberately specific compounds ("api_key", not bare "key") so genuine
// non-secret identifiers such as dedup_key or idempotency_key survive.
var credentialKeyMarkers = []string{
	"secret",
	"password",
	"passwd",
	"credential",
	"api_key",
	"apikey",
	"access_key",
	"secret_key",
	"anon_key",
	"private_key",
}

// IsSecretKey reports whether a field, param, or log-attr name holds a
// credential. It is the one vocabulary behind JSON body scrubbing here and attr
// dropping in internal/shared/logging, so a name learned in one place is
// masked in the other.
func IsSecretKey(name string) bool {
	k := strings.ToLower(name)
	// Any secret token field ends in "token" (accessToken, discogs_token);
	// a suffix match keeps non-secret counters like "token_count" intact.
	if strings.HasSuffix(k, "token") {
		return true
	}
	for _, marker := range credentialKeyMarkers {
		if strings.Contains(k, marker) {
			return true
		}
	}
	return false
}
