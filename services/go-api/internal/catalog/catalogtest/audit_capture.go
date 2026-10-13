package catalogtest

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type AuditRecord struct {
	Msg   string
	Time  time.Time
	Attrs map[string]string
}

type AuditCapture struct {
	mu      sync.Mutex
	records []AuditRecord
}

func (h *AuditCapture) Enabled(context.Context, slog.Level) bool { return true }

func (h *AuditCapture) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *AuditCapture) WithGroup(string) slog.Handler { return h }

func (h *AuditCapture) Handle(_ context.Context, r slog.Record) error {
	rec := AuditRecord{Msg: r.Message, Time: r.Time, Attrs: map[string]string{}}
	r.Attrs(func(a slog.Attr) bool {
		rec.Attrs[a.Key] = a.Value.String()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, rec)
	return nil
}

func (h *AuditCapture) Find(t *testing.T, msg string) AuditRecord {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Msg == msg {
			return r
		}
	}
	t.Fatalf("no %q log line; destructive action left no attributable trail", msg)
	return AuditRecord{}
}

func CaptureAuditLogs(t *testing.T) *AuditCapture {
	t.Helper()
	h := &AuditCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

func AssertAttrs(t *testing.T, rec AuditRecord, want map[string]string) {
	t.Helper()
	if rec.Time.IsZero() {
		t.Errorf("%q: record has no time", rec.Msg)
	}
	for k, v := range want {
		if got := rec.Attrs[k]; got != v {
			t.Errorf("%q: attr %s = %q, want %q", rec.Msg, k, got, v)
		}
	}
}
