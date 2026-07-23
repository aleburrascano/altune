package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// --- fakes ---

type fakeChartProvider struct {
	entries []domain.VocabularyEntry
	err     error
}

func (f *fakeChartProvider) FetchCharts(_ context.Context, _ int) ([]domain.VocabularyEntry, error) {
	return f.entries, f.err
}

type fakeVocabularyStore struct {
	bulkAdded []domain.VocabularyEntry
	err       error
	// Optional read-side overrides (used by suggest/correction tests); nil keeps
	// the no-result default the refresh tests rely on. Call counts record either way.
	suggestByPrefixFn func(prefix string, limit int) ([]domain.VocabularyEntry, error)
	findClosestFn     func(query string, limit int) ([]domain.VocabularyEntry, error)
	addFn             func(entry domain.VocabularyEntry) error
	suggestCalls      int
	findClosestCalls  int
}

func (f *fakeVocabularyStore) Add(_ context.Context, entry domain.VocabularyEntry) error {
	if f.addFn != nil {
		return f.addFn(entry)
	}
	return nil
}

func (f *fakeVocabularyStore) BulkAdd(_ context.Context, entries []domain.VocabularyEntry) error {
	f.bulkAdded = append(f.bulkAdded, entries...)
	return f.err
}

func (f *fakeVocabularyStore) SuggestByPrefix(_ context.Context, prefix string, limit int) ([]domain.VocabularyEntry, error) {
	f.suggestCalls++
	if f.suggestByPrefixFn != nil {
		return f.suggestByPrefixFn(prefix, limit)
	}
	return nil, nil
}

func (f *fakeVocabularyStore) FindClosest(_ context.Context, query string, limit int) ([]domain.VocabularyEntry, error) {
	f.findClosestCalls++
	if f.findClosestFn != nil {
		return f.findClosestFn(query, limit)
	}
	return nil, nil
}

// --- tests ---

func TestVocabularyRefresh_HappyPath(t *testing.T) {
	store := &fakeVocabularyStore{}
	charts := []fakeChartProvider{
		{entries: []domain.VocabularyEntry{
			{Term: "Drake", Kind: "artist", Popularity: 1000},
		}},
		{entries: []domain.VocabularyEntry{
			{Term: "Blinding Lights", Kind: "track", Popularity: 500},
		}},
	}
	svc := newTestRefreshService(charts, store)

	err := svc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.bulkAdded) != 2 {
		t.Fatalf("got %d entries, want 2", len(store.bulkAdded))
	}
	assertEntryTerm(t, store.bulkAdded[0], "Drake")
	assertEntryTerm(t, store.bulkAdded[1], "Blinding Lights")
}

func TestVocabularyRefresh_NormalizesTerms(t *testing.T) {
	store := &fakeVocabularyStore{}
	charts := []fakeChartProvider{
		{entries: []domain.VocabularyEntry{
			{Term: "The Weeknd", Kind: "artist", Popularity: 900},
		}},
	}
	svc := newTestRefreshService(charts, store)

	err := svc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.bulkAdded) != 1 {
		t.Fatalf("got %d entries, want 1", len(store.bulkAdded))
	}
	if store.bulkAdded[0].TermNorm == "" {
		t.Error("TermNorm should be set after normalization")
	}
	if store.bulkAdded[0].TermNorm == store.bulkAdded[0].Term {
		t.Error("TermNorm should differ from Term for 'The Weeknd'")
	}
}

func TestVocabularyRefresh_OneProviderFails(t *testing.T) {
	store := &fakeVocabularyStore{}
	charts := []fakeChartProvider{
		{err: errors.New("network timeout")},
		{entries: []domain.VocabularyEntry{
			{Term: "Bad Bunny", Kind: "artist", Popularity: 800},
		}},
	}
	svc := newTestRefreshService(charts, store)

	err := svc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.bulkAdded) != 1 {
		t.Fatalf("got %d entries, want 1", len(store.bulkAdded))
	}
	assertEntryTerm(t, store.bulkAdded[0], "Bad Bunny")
}

func TestVocabularyRefresh_AllProvidersFail(t *testing.T) {
	store := &fakeVocabularyStore{}
	charts := []fakeChartProvider{
		{err: errors.New("error 1")},
		{err: errors.New("error 2")},
	}
	svc := newTestRefreshService(charts, store)

	err := svc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.bulkAdded) != 0 {
		t.Fatalf("got %d entries, want 0", len(store.bulkAdded))
	}
}

func TestVocabularyRefresh_EmptyResults(t *testing.T) {
	store := &fakeVocabularyStore{}
	charts := []fakeChartProvider{
		{entries: []domain.VocabularyEntry{}},
	}
	svc := newTestRefreshService(charts, store)

	err := svc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.bulkAdded) != 0 {
		t.Fatalf("got %d entries, want 0", len(store.bulkAdded))
	}
}

func TestVocabularyRefresh_StoreError(t *testing.T) {
	store := &fakeVocabularyStore{err: errors.New("redis down")}
	charts := []fakeChartProvider{
		{entries: []domain.VocabularyEntry{
			{Term: "Taylor Swift", Kind: "artist", Popularity: 999},
		}},
	}
	svc := newTestRefreshService(charts, store)

	err := svc.RunOnce(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestVocabularyRefresh_DuplicateTerms(t *testing.T) {
	store := &fakeVocabularyStore{}
	charts := []fakeChartProvider{
		{entries: []domain.VocabularyEntry{
			{Term: "Drake", Kind: "artist", Popularity: 1000},
		}},
		{entries: []domain.VocabularyEntry{
			{Term: "Drake", Kind: "artist", Popularity: 900},
		}},
	}
	svc := newTestRefreshService(charts, store)

	err := svc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both entries passed to BulkAdd; dedup is the store's concern
	if len(store.bulkAdded) != 2 {
		t.Fatalf("got %d entries, want 2", len(store.bulkAdded))
	}
}

func TestVocabularyRefresh_StartTwiceAndShutdownSafe(t *testing.T) {
	// A second Start must be a no-op (the old code double-closed done and leaked
	// the first cancel), and Shutdown must stop the single loop cleanly.
	store := &fakeVocabularyStore{}
	svc := NewVocabularyRefreshService(nil, store, time.Hour, 50)

	svc.Start()
	svc.Start() // no-op, must not panic on shutdown's close(done)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	svc.Shutdown(ctx)
	if ctx.Err() != nil {
		t.Fatal("shutdown timed out (loop never finished)")
	}
}

func TestVocabularyRefresh_ShutdownBeforeStartReturns(t *testing.T) {
	store := &fakeVocabularyStore{}
	svc := NewVocabularyRefreshService(nil, store, time.Hour, 50)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	svc.Shutdown(ctx) // never started: must return immediately, not hang on done
	if ctx.Err() != nil {
		t.Fatal("Shutdown before Start blocked until the context expired")
	}
}

// --- helpers ---

func newTestRefreshService(
	charts []fakeChartProvider,
	store *fakeVocabularyStore,
) *VocabularyRefreshService {
	providers := make([]ports.ChartProvider, len(charts))
	for i := range charts {
		providers[i] = &charts[i]
	}
	return NewVocabularyRefreshService(
		providers, store, 1, 50,
	)
}

func assertEntryTerm(t *testing.T, entry domain.VocabularyEntry, want string) {
	t.Helper()
	if entry.Term != want {
		t.Errorf("got term %q, want %q", entry.Term, want)
	}
}
