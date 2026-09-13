package logging

import "log/slog"

// sensitiveLogKeys lists attribute keys whose values must never reach the ring
// buffer or the underlying handler. Extend this list to redact more fields;
// nothing here affects ring-buffer capacity or handler wiring.
var sensitiveLogKeys = map[string]struct{}{
	"query": {},
}

func withoutSensitiveAttrs(r slog.Record) slog.Record {
	present := false
	r.Attrs(func(a slog.Attr) bool {
		if _, ok := sensitiveLogKeys[a.Key]; ok {
			present = true
			return false
		}
		return true
	})
	if !present {
		return r
	}
	clean := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		if _, ok := sensitiveLogKeys[a.Key]; !ok {
			clean.AddAttrs(a)
		}
		return true
	})
	return clean
}
