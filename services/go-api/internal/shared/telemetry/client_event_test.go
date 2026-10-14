package telemetry

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/embedded"
)

type recordingLogger struct {
	embedded.Logger
	records []log.Record
}

func (l *recordingLogger) Emit(_ context.Context, r log.Record) { l.records = append(l.records, r) }
func (l *recordingLogger) Enabled(context.Context, log.EnabledParameters) bool {
	return true
}

func attrMap(r log.Record) map[string]attribute.Value {
	out := map[string]attribute.Value{}
	r.WalkAttributes(func(kv attribute.KeyValue) bool {
		out[string(kv.Key)] = kv.Value
		return true
	})
	return out
}

func emitOne(t *testing.T, key string, e ClientEvent) (log.Record, map[string]attribute.Value) {
	t.Helper()
	l := &recordingLogger{}
	NewClientEvents(key, l).Emit(context.Background(), e)
	if len(l.records) != 1 {
		t.Fatalf("records = %d, want 1", len(l.records))
	}
	return l.records[0], attrMap(l.records[0])
}

func TestClientEvents_ScrubsPayload(t *testing.T) {
	rec, attrs := emitOne(t, "", ClientEvent{
		Type: "search_submitted", EventID: "e1", AppVersion: "1.2.3", Platform: "ios",
		Payload: map[string]any{
			"title": "Daft Punk", "message": "boom", "surface": "search", "position": float64(3),
			"will_retry": true, "results": []any{1, 2}, "session_id": "s-1",
		},
	})
	if rec.Body().AsString() != "client_event" || rec.Severity() != log.SeverityInfo {
		t.Errorf("body/severity = %q/%v", rec.Body().AsString(), rec.Severity())
	}
	for _, k := range []string{"payload.surface", "payload.position", "payload.will_retry", "event.type", "event.id", "app.version", "app.platform", "session.id"} {
		if _, ok := attrs[k]; !ok {
			t.Errorf("missing attribute %s", k)
		}
	}
	for _, k := range []string{"payload.title", "payload.message", "payload.results"} {
		if _, ok := attrs[k]; ok {
			t.Errorf("attribute %s must be dropped", k)
		}
	}
}

func TestClientEvents_StringsOutsideAllowListDropped(t *testing.T) {
	_, attrs := emitOne(t, "", ClientEvent{Type: "x", Payload: map[string]any{"query": "secret", "error": "e", "session_id": "s"}})
	for k := range attrs {
		if strings.HasPrefix(k, "payload.") {
			t.Errorf("unexpected attribute %s", k)
		}
	}
}

func TestClientEvents_OmitsEmptyEventID(t *testing.T) {
	_, attrs := emitOne(t, "", ClientEvent{Type: "x"})
	if _, ok := attrs["event.id"]; ok {
		t.Error("event.id must be absent when empty")
	}
}

func TestClientEvents_PseudonymisesUser(t *testing.T) {
	_, attrs := emitOne(t, "k", ClientEvent{Type: "x", UserID: "u-123"})
	id := attrs["user.pseudo_id"].AsString()
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(id) {
		t.Errorf("user.pseudo_id = %q, want 16 hex chars", id)
	}
	for k, v := range attrs {
		if strings.Contains(k, "u-123") || strings.Contains(v.String(), "u-123") {
			t.Errorf("attribute %s leaks the raw user id", k)
		}
	}
	_, again := emitOne(t, "k", ClientEvent{Type: "x", UserID: "u-123"})
	if again["user.pseudo_id"].AsString() != id {
		t.Error("pseudo id must be stable")
	}
}

func TestClientEvents_NoKeyNoUserAttribute(t *testing.T) {
	_, attrs := emitOne(t, "", ClientEvent{Type: "x", UserID: "u-123"})
	for k, v := range attrs {
		if strings.HasPrefix(k, "user.") || strings.Contains(v.String(), "u-123") {
			t.Errorf("unexpected user attribute %s", k)
		}
	}
}
