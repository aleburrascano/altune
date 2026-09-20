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

// scrubbedRecord returns a record with every secret-bearing attr dropped,
// group members included, and the credentials embedded in the free text that
// remains masked. The record it returns feeds both the ring and the handler
// writing the persisted stream, so the two cannot disagree on what an operator
// may see.
func scrubbedRecord(r slog.Record) slog.Record {
	clean := slog.NewRecord(r.Time, r.Level, scrubSecrets(r.Message), r.PC)
	clean.AddAttrs(withoutSensitiveLeaves("", recordAttrs(r))...)
	return clean
}

func recordAttrs(r slog.Record) []slog.Attr {
	attrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	return attrs
}

// withoutSensitiveLeaves drops every secret-bearing leaf from attrs, judging
// group members by their dotted key exactly as flattenAttr does, so a secret
// the ring redacts can never survive into the persisted stream instead.
func withoutSensitiveLeaves(prefix string, attrs []slog.Attr) []slog.Attr {
	kept := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		if safe, survives := attrWithoutSensitiveMembers(prefix, a); survives {
			kept = append(kept, safe)
		}
	}
	return kept
}

// attrWithoutSensitiveMembers resolves a once, so a LogValuer cannot pass the
// check here and hand a secret to the handler at format time.
func attrWithoutSensitiveMembers(prefix string, a slog.Attr) (safe slog.Attr, survives bool) {
	val := a.Value.Resolve()
	key := dottedKey(prefix, a.Key)
	if val.Kind() == slog.KindGroup {
		members := withoutSensitiveLeaves(key, val.Group())
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(members...)}, len(members) > 0
	}
	return slog.Attr{Key: a.Key, Value: scrubbedValue(val)}, !isSensitiveLeaf(key, val)
}

// scrubbedValue masks the credentials inside a leaf whose key names nothing
// secret: a *url.Error logged under the conventional "error" key carries the
// provider api_key in its URL, and only reading the value catches that. A leaf
// with nothing to mask keeps its own kind, so a number stays a number in the
// persisted stream.
func scrubbedValue(v slog.Value) slog.Value {
	text := v.String()
	scrubbed := scrubSecrets(text)
	if scrubbed == text {
		return v
	}
	return slog.StringValue(scrubbed)
}
