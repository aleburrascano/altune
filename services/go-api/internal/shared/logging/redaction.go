package logging

import (
	"log/slog"
	"regexp"
	"strings"
)

// sensitiveKeyMarkers is the vocabulary of secret-bearing log attr keys the
// codebase actually uses: every Config secret field (tokens, API keys, access
// and secret keys) plus the raw "query" text captured at the search boundary.
// A key is redacted when its lower-cased form contains any of these markers.
// Markers are deliberately specific compounds (e.g. "api_key", not bare "key")
// so genuine non-secret identifiers such as dedup_key or idempotency_key are
// left intact.
var sensitiveKeyMarkers = []string{
	"query",
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

// credentialURL matches a URL that embeds userinfo credentials, e.g.
// "redis://user:pass@host" or "postgres://u:p@h" — the shape RedisURL and
// DatabaseURL carry. Matching the value (not just the key) also catches a
// credential URL that leaks through a generic "error" or "detail" attr.
var credentialURL = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.\-]*://[^\s/:@]+:[^\s/@]+@`)

func isSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	// Any secret token field ends in "token" (access_token, discogs_token);
	// a suffix match keeps non-secret counters like "token_count" intact.
	if strings.HasSuffix(k, "token") {
		return true
	}
	for _, marker := range sensitiveKeyMarkers {
		if strings.Contains(k, marker) {
			return true
		}
	}
	return false
}

func isSensitiveAttr(a slog.Attr) bool {
	if isSensitiveKey(a.Key) {
		return true
	}
	v := a.Value.Resolve()
	return v.Kind() == slog.KindString && credentialURL.MatchString(v.String())
}

func hasSensitiveAttr(r slog.Record) bool {
	present := false
	r.Attrs(func(a slog.Attr) bool {
		if isSensitiveAttr(a) {
			present = true
			return false
		}
		return true
	})
	return present
}

// withoutSensitiveAttrs returns a record with every secret-bearing attr dropped.
// It is the single redaction choke point on the ring-buffer log path.
func withoutSensitiveAttrs(r slog.Record) slog.Record {
	if !hasSensitiveAttr(r) {
		return r
	}
	clean := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		if !isSensitiveAttr(a) {
			clean.AddAttrs(a)
		}
		return true
	})
	return clean
}
