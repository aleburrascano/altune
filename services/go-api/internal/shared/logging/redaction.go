package logging

import (
	"altune/go-api/internal/shared/redact"
	"log/slog"
	"regexp"
	"strings"
)

// credentialURL matches a URL that embeds userinfo credentials, e.g.
// "redis://user:pass@host" or "postgres://u:p@h" — the shape RedisURL and
// DatabaseURL carry. Matching the value (not just the key) also catches a
// credential URL that leaks through a generic "error" or "detail" attr.
var credentialURL = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^\s/:@]+:[^\s/@]+@`)

// scrubSecrets masks credentials embedded in free text bound for the ring: the
// values of secret query params (e.g. a Last.fm api_key inside a *url.Error)
// and userinfo in credential URLs. Unlike key-based dropping, it inspects the
// text itself, so it covers Message and generic attrs such as "error".
func scrubSecrets(s string) string {
	return credentialURL.ReplaceAllString(redact.Secrets(s), "${1}REDACTED@")
}

// isSensitiveLeaf reports whether a flattened attr must be dropped from the
// ring: its full dotted key names a secret, or its string value is a
// credential URL.
func isSensitiveLeaf(key string, v slog.Value) bool {
	if isSensitiveKey(key) {
		return true
	}
	return v.Kind() == slog.KindString && credentialURL.MatchString(v.String())
}

// isSensitiveKey defers the credential vocabulary to redact.IsSecretKey, which
// httptrace body scrubbing shares, and adds the one marker that is log-only:
// "query" is the raw search text captured at the search boundary — a privacy
// concern here, not a credential.
func isSensitiveKey(key string) bool {
	return redact.IsSecretKey(key) || strings.Contains(strings.ToLower(key), "query")
}

func isSensitiveAttr(a slog.Attr) bool {
	return isSensitiveLeaf(a.Key, a.Value.Resolve())
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
