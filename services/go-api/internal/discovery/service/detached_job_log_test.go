package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// Issue #2244: work the search pipeline detaches from the request — the
// telemetry emit, the history write, and any panic inside them — used to fail
// into a line an operator could not act on. These tests pin what each failure
// line must carry to be diagnosable without a reproduction.

// captureDetachedLogs installs the production handler chain and returns its
// ring. The bare JSON handler captureProductionLogs installs stamps no
// correlation id and applies no attr redaction, so it could not tell a line
// that reaches an operator from one the redaction filter drops. Stdout stays at
// Error so only the panic line is echoed into test output.
func captureDetachedLogs(t *testing.T) *logging.RingBuffer {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	return logging.Setup("error", false)
}

func onlyRecord(t *testing.T, ring *logging.RingBuffer, msg string) logging.CapturedRecord {
	t.Helper()
	var found []logging.CapturedRecord
	for _, r := range ring.Snapshot() {
		if r.Message == msg {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		t.Fatalf("captured %d %q records, want exactly 1", len(found), msg)
	}
	return found[0]
}

func TestBackgroundLaunch_PanicLogsErrorWithCorrelationIDAndStack(t *testing.T) {
	ring := captureDetachedLogs(t)
	const corrID = "corr-bg-2244"
	ctx := logging.WithCorrelationID(context.Background(), corrID)

	var bg backgroundRunner
	bg.launch(ctx, "telemetry.emit", func(context.Context) { panic("adapter exploded") })
	bg.wait()

	rec := onlyRecord(t, ring, "search.v2.background_panic")
	if rec.Level != slog.LevelError.String() {
		t.Errorf("level = %s, want ERROR: a crashed background job is not a warning", rec.Level)
	}
	if rec.Attrs["corr_id"] != corrID {
		t.Errorf("corr_id = %q, want %q: the log call must carry the detached context", rec.Attrs["corr_id"], corrID)
	}
	if !strings.Contains(rec.Attrs["stack"], "TestBackgroundLaunch_PanicLogsErrorWithCorrelationIDAndStack") {
		t.Errorf("stack = %q, want the frames of the function that panicked", rec.Attrs["stack"])
	}
	if !strings.Contains(rec.Attrs["panic"], "adapter exploded") {
		t.Errorf("panic = %q, want the recovered value", rec.Attrs["panic"])
	}
	if rec.Attrs["label"] != "telemetry.emit" {
		t.Errorf("label = %q, want telemetry.emit: the line must name which job died", rec.Attrs["label"])
	}
}

func TestSearchTelemetry_DroppedEventLogsSearchAndUser(t *testing.T) {
	store := &fakeEventStore{err: errors.New("db down")}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithEventStore(store))
	user := newUser()
	ring := captureDetachedLogs(t)

	out, err := svc.Execute(context.Background(), user, newQuery(t, "humble"), false)
	svc.WaitForBackground()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rec := onlyRecord(t, ring, "search.v2.telemetry_emit_failed")
	if rec.Attrs["search_id"] != out.SearchId {
		t.Errorf("search_id = %q, want %q: the dropped event is unfindable without it", rec.Attrs["search_id"], out.SearchId)
	}
	if rec.Attrs["user_id"] != user.String() {
		t.Errorf("user_id = %q, want %q", rec.Attrs["user_id"], user.String())
	}
	if !strings.Contains(rec.Attrs["error"], "db down") {
		t.Errorf("error = %q, want the store's failure", rec.Attrs["error"])
	}
}

func TestRecordHistory_InsertFailureLogsUserAndSearchFingerprint(t *testing.T) {
	repo := &fakeHistoryWriter{
		insertFn: func(context.Context, *domain.SearchHistoryEntry) error {
			return errors.New("db down")
		},
	}
	user := newUser()
	const raw = "zqxj private diagnosis clinic"
	ring := captureDetachedLogs(t)

	NewRecordSearchHistoryService(repo).Record(context.Background(), user, newQuery(t, raw), raw, true)

	rec := onlyRecord(t, ring, "search.v2.history_persist_failed")
	if rec.Attrs["user_id"] != user.String() {
		t.Errorf("user_id = %q, want %q: the line must name whose history was lost", rec.Attrs["user_id"], user.String())
	}
	if rec.Attrs["search_text.fp"] == "" {
		t.Errorf("no search_text fingerprint to join the failure to the search, attrs = %v", rec.Attrs)
	}
	// The fingerprint stands in for text clear-history promises to erase (#1097).
	for _, form := range []string{raw, "diagnosis"} {
		if strings.Contains(rec.Attrs["search_text.fp"]+rec.Attrs["error"], form) {
			t.Errorf("failure line leaks search text %q: %v", form, rec.Attrs)
		}
	}
}
