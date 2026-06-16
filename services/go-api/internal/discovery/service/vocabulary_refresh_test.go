package service

import (
	"context"
	"errors"
	"testing"

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
}

func (f *fakeVocabularyStore) Add(_ context.Context, _ domain.VocabularyEntry) error {
	return nil
}

func (f *fakeVocabularyStore) BulkAdd(_ context.Context, entries []domain.VocabularyEntry) error {
	f.bulkAdded = append(f.bulkAdded, entries...)
	return f.err
}

func (f *fakeVocabularyStore) SuggestByPrefix(_ context.Context, _ string, _ int) ([]domain.VocabularyEntry, error) {
	return nil, nil
}

func (f *fakeVocabularyStore) FindClosest(_ context.Context, _ string, _ int) ([]domain.VocabularyEntry, error) {
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
