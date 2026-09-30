package logging

import (
	"altune/go-api/internal/shared/redact"
	"log/slog"
	"regexp"
	"strings"
)

var credentialURL = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^\s/:@]+:[^\s/@]+@`)

func scrubSecrets(s string) string {
	return credentialURL.ReplaceAllString(redact.Secrets(s), "${1}"+redact.Mask+"@")
}

func isSensitiveLeaf(key string, v slog.Value) bool {
	if isSensitiveKey(key) {
		return true
	}
	return v.Kind() == slog.KindString && credentialURL.MatchString(v.String())
}

func isSensitiveKey(key string) bool {
	return redact.IsSecretKey(key) || strings.Contains(strings.ToLower(key), "query")
}

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

func withoutSensitiveLeaves(prefix string, attrs []slog.Attr) []slog.Attr {
	kept := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		if safe, survives := attrWithoutSensitiveMembers(prefix, a); survives {
			kept = append(kept, safe)
		}
	}
	return kept
}

func attrWithoutSensitiveMembers(prefix string, a slog.Attr) (safe slog.Attr, survives bool) {
	val := a.Value.Resolve()
	key := dottedKey(prefix, a.Key)
	if val.Kind() == slog.KindGroup {
		members := withoutSensitiveLeaves(key, val.Group())
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(members...)}, len(members) > 0
	}
	return slog.Attr{Key: a.Key, Value: scrubbedValue(a.Key, val)}, !isSensitiveLeaf(key, val)
}

func carriesFailureText(key string, v slog.Value) bool {
	switch strings.ToLower(key) {
	case "error", "err", "panic", "stack":
		return true
	}
	if v.Kind() != slog.KindAny {
		return false
	}
	_, isErr := v.Any().(error)
	return isErr
}

func scrubbedValue(key string, v slog.Value) slog.Value {
	text := v.String()
	scrubbed := scrubSecrets(text)
	if carriesFailureText(key, v) {
		scrubbed = redact.LogText(scrubbed)
	}
	if scrubbed == text {
		return v
	}
	return slog.StringValue(scrubbed)
}
