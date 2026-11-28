package service_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/clock/clocktest"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/ids/idstest"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRecordHistory_RefusesAMissingRepository(t *testing.T) {
	if _, err := service.NewRecordSearchHistoryService(nil, time.Now, ids.Real); !errors.Is(err, service.ErrHistoryCollaboratorRequired) {
		t.Fatalf("err = %v, want history.ErrCollaboratorRequired", err)
	}
}

func TestRecordHistory_InsertFailureLogsUserAndSearchFingerprint(t *testing.T) {
	repo := &memory.FakeHistoryWriter{
		InsertFn: func(context.Context, *domain.SearchHistoryEntry) error {
			return errors.New("db down")
		},
	}
	user := memory.NewUser()
	const raw = "zqxj private diagnosis clinic"
	ring := memory.CaptureDetachedLogs(t)

	searchtest.NewHistoryRecorder(t, repo).Record(logging.ContextWithSubjectUser(context.Background(), user.String()), user, memory.NewQuery(t, raw), raw, time.Now())

	rec := memory.OnlyRecord(t, ring, "search.v2.history_persist_failed")
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
	repo := &memory.FakeHistoryWriter{
		InsertFn: func(_ context.Context, e *domain.SearchHistoryEntry) error {
			got = e.ExecutedAt
			return nil
		},
	}
	svc := service.MustNewRecordSearchHistoryService(repo, clocktest.NewManual(at).Func(), ids.Real)

	if err := svc.RecordSubmitted(context.Background(), memory.NewUser(), "some query"); err != nil {
		t.Fatalf("RecordSubmitted: %v", err)
	}

	if !got.Equal(at) || got.Location() != time.UTC {
		t.Errorf("ExecutedAt = %v, want %v in UTC", got, at.UTC())
	}
}

func TestNewRecordSearchHistoryService_RefusesANilClock(t *testing.T) {
	svc, err := service.NewRecordSearchHistoryService(&memory.FakeHistoryWriter{}, nil, ids.Real)

	if !errors.Is(err, service.ErrClockRequired) || svc != nil {
		t.Fatalf("NewRecordSearchHistoryService(nil clock) = (%v, %v), want history.ErrClockRequired and no service", svc, err)
	}
}

func TestNewRecordSearchHistoryService_RefusesANilIDSource(t *testing.T) {
	svc, err := service.NewRecordSearchHistoryService(&memory.FakeHistoryWriter{}, time.Now, nil)

	if !errors.Is(err, service.ErrIDSourceRequired) || svc != nil {
		t.Fatalf("NewRecordSearchHistoryService(nil id source) = (%v, %v), want history.ErrIDSourceRequired and no service", svc, err)
	}
}

func TestRecordHistory_EntryIdComesFromTheInjectedSource(t *testing.T) {
	var stored *domain.SearchHistoryEntry
	repo := &memory.FakeHistoryWriter{
		InsertFn: func(_ context.Context, e *domain.SearchHistoryEntry) error {
			stored = e
			return nil
		},
	}
	sequence := idstest.Sequence()
	svc := service.MustNewRecordSearchHistoryService(repo, time.Now, sequence)

	svc.Record(context.Background(), memory.NewUser(), memory.NewQuery(t, "humble"), "humble", time.Now())

	if stored == nil || stored.ID != idstest.Sequence().NewEntityId() {
		t.Errorf("entry id = %v, want the first id of the injected sequence", stored)
	}
}
