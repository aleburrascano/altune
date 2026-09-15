package handler

import (
	"altune/go-api/internal/shared/logging"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	discdomain "altune/go-api/internal/discovery/domain"

	"github.com/google/uuid"
)

// secretSearchText is seeded into history so the audit tests can prove the
// erased search text never reaches the logs (#1097).
const secretSearchText = "very private query 7f3a"

func seededHistoryRepo() *fakeSearchHistoryRepo {
	return &fakeSearchHistoryRepo{entries: []*discdomain.SearchHistoryEntry{{
		ID:         uuid.New(),
		UserId:     discTestUserId,
		Query:      secretSearchText,
		QueryNorm:  secretSearchText,
		ExecutedAt: time.Now().UTC(),
	}}}
}

func captureLogs(t *testing.T) *logging.RingBuffer {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	return logging.Setup("info", false)
}

func findRecord(ring *logging.RingBuffer, msg string) (logging.CapturedRecord, bool) {
	for _, r := range ring.Snapshot() {
		if r.Message == msg {
			return r, true
		}
	}
	return logging.CapturedRecord{}, false
}

func assertNoSearchText(t *testing.T, ring *logging.RingBuffer) {
	t.Helper()
	for _, r := range ring.Snapshot() {
		if strings.Contains(r.Message, secretSearchText) {
			t.Fatalf("log message leaks search text: %q", r.Message)
		}
		for k, v := range r.Attrs {
			if strings.Contains(v, secretSearchText) {
				t.Fatalf("log attr %s leaks search text: %q", k, v)
			}
		}
	}
}

// TestHandleClearSearchHistory_AuditsSuccess pins #1101: a successful clear
// leaves an actor-attributed, timestamped audit record without the erased text.
func TestHandleClearSearchHistory_AuditsSuccess(t *testing.T) {
	ring := captureLogs(t)
	router := buildDiscoveryRouter(nil, seededHistoryRepo(), nil, nil)

	before := time.Now().UTC()
	rec := discServe(t, router, http.MethodDelete, "/discovery/search-history", nil)
	discAssertStatus(t, rec, http.StatusNoContent)

	r, ok := findRecord(ring, "discovery.search_history_cleared")
	if !ok {
		t.Fatalf("no discovery.search_history_cleared audit record; logs: %v", ring.Snapshot())
	}
	if r.Level != "INFO" {
		t.Errorf("level = %q, want INFO", r.Level)
	}
	if r.Attrs["user_id"] != discTestUserId.String() {
		t.Errorf("user_id = %q, want %q", r.Attrs["user_id"], discTestUserId.String())
	}
	if r.Attrs["action"] != "clear_search_history" {
		t.Errorf("action = %q, want clear_search_history", r.Attrs["action"])
	}
	// The ring renders slog time values with time.Time.String().
	at, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", r.Attrs["at"])
	if err != nil {
		t.Fatalf("at = %q is not a timestamp: %v", r.Attrs["at"], err)
	}
	if at.Before(before.Add(-time.Second)) || at.After(time.Now().UTC().Add(time.Second)) {
		t.Errorf("at = %v, want around request time %v", at, before)
	}
	assertNoSearchText(t, ring)
}

// TestHandleClearSearchHistory_AuditsFailure pins #1101: a failed clear names
// the user on the error line and emits no success audit record.
func TestHandleClearSearchHistory_AuditsFailure(t *testing.T) {
	ring := captureLogs(t)
	repo := seededHistoryRepo()
	repo.err = errors.New("db unavailable")
	router := buildDiscoveryRouter(nil, repo, nil, nil)

	rec := discServe(t, router, http.MethodDelete, "/discovery/search-history", nil)
	discAssertStatus(t, rec, http.StatusInternalServerError)

	r, ok := findRecord(ring, "clear search history failed")
	if !ok {
		t.Fatalf("no clear search history failed record; logs: %v", ring.Snapshot())
	}
	if r.Attrs["user_id"] != discTestUserId.String() {
		t.Errorf("user_id = %q, want %q", r.Attrs["user_id"], discTestUserId.String())
	}
	if r.Attrs["action"] != "clear_search_history" {
		t.Errorf("action = %q, want clear_search_history", r.Attrs["action"])
	}
	if _, ok := findRecord(ring, "discovery.search_history_cleared"); ok {
		t.Error("success audit record emitted for a failed clear")
	}
	assertNoSearchText(t, ring)
}
