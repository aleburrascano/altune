package service

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"strings"
	"testing"
)

// Regression test for #2244: a detached job's failure line must carry what an
// operator needs to diagnose it without a reproduction.
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
