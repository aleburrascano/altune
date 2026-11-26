package history_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/clock/clocktest"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRecordHistory_RefusesAMissingRepository(t *testing.T) {
	if _, err := search.NewRecordSearchHistoryService(nil, time.Now); !errors.Is(err, search.ErrHistoryCollaboratorRequired) {
		t.Fatalf("err = %v, want ErrHistoryCollaboratorRequired", err)
	}
}

func TestRecordHistory_InsertFailureLogsUserAndSearchFingerprint(t *testing.T) {
	repo := &search.FakeHistoryWriter{
		InsertFn: func(context.Context, *search.SearchHistoryEntry) error {
			return errors.New("db down")
		},
	}
	user := search.NewUser()
	const raw = "zqxj private diagnosis clinic"
	ring := search.CaptureDetachedLogs(t)

	searchtest.NewHistoryRecorder(t, repo).Record(context.Background(), user, search.NewQuery(t, raw), raw, time.Now())

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

func TestRecordSubmitted_StampsTheEntryWithTheInjectedClockInUTC(t *testing.T) {
	at := time.Date(2026, 7, 8, 9, 10, 11, 0, time.FixedZone("plus2", 2*60*60))
	var got time.Time
	repo := &search.FakeHistoryWriter{
		InsertFn: func(_ context.Context, e *search.SearchHistoryEntry) error {
			got = e.ExecutedAt
			return nil
		},
	}
	svc := search.MustNewRecordSearchHistoryService(repo, clocktest.NewManual(at).Func())

	if err := svc.RecordSubmitted(context.Background(), search.NewUser(), "some query"); err != nil {
		t.Fatalf("RecordSubmitted: %v", err)
	}

	if !got.Equal(at) || got.Location() != time.UTC {
		t.Errorf("ExecutedAt = %v, want %v in UTC", got, at.UTC())
	}
}

func TestNewRecordSearchHistoryService_RefusesANilClock(t *testing.T) {
	svc, err := search.NewRecordSearchHistoryService(&search.FakeHistoryWriter{}, nil)

	if !errors.Is(err, search.ErrHistoryClockRequired) || svc != nil {
		t.Fatalf("NewRecordSearchHistoryService(nil clock) = (%v, %v), want ErrHistoryClockRequired and no service", svc, err)
	}
}
