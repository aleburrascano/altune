package history_test

import (
	"altune/go-api/internal/search"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRecordHistory_InsertFailureLogsUserAndSearchFingerprint(t *testing.T) {
	repo := &search.FakeHistoryWriter{
		InsertFn: func(context.Context, *search.SearchHistoryEntry) error {
			return errors.New("db down")
		},
	}
	user := search.NewUser()
	const raw = "zqxj private diagnosis clinic"
	ring := search.CaptureDetachedLogs(t)

	search.NewRecordSearchHistoryService(repo).Record(context.Background(), user, search.NewQuery(t, raw), raw, true, time.Now())

	rec := search.OnlyRecord(t, ring, "search.v2.history_persist_failed")
	if rec.Attrs["user_id"] != user.String() {
		t.Errorf("user_id = %q, want %q: the line must name whose history was lost", rec.Attrs["user_id"], user.String())
	}
	if rec.Attrs["search_text.fp"] == "" {
		t.Errorf("no search_text fingerprint to join the failure to the search, attrs = %v", rec.Attrs)
	}
	for _, form := range []string{raw, "diagnosis"} {
		if strings.Contains(rec.Attrs["search_text.fp"]+rec.Attrs["error"], form) {
			t.Errorf("failure line leaks search text %q: %v", form, rec.Attrs)
		}
	}
}
