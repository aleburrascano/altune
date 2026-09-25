package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
)

type fakeChartProvider struct {
	name    domain.ProviderName
	entries []domain.VocabularyEntry
	err     error
}

func (f *fakeChartProvider) Name() domain.ProviderName { return f.name }

func (f *fakeChartProvider) FetchCharts(_ context.Context, _ int) ([]domain.VocabularyEntry, error) {
	return f.entries, f.err
}

type fakeVocabularyStore struct {
	bulkAdded         []domain.VocabularyEntry
	err               error
	suggestByPrefixFn func(prefix string, limit int) ([]domain.VocabularyEntry, error)
	findClosestFn     func(query string, limit int) ([]domain.VocabularyEntry, error)
	addFn             func(entry domain.VocabularyEntry) error
	suggestCalls      int
	findClosestCalls  int
	trimCalls         int
	trimmedTo         int
}

func (f *fakeVocabularyStore) Trim(_ context.Context, maxEntries int) error {
	f.trimCalls++
	f.trimmedTo = maxEntries
	return nil
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
		{name: domain.ProviderLastFM, err: errors.New("network timeout")},
		{name: domain.ProviderDeezer, entries: []domain.VocabularyEntry{
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

// Issue #2243: a refresh where every chart provider failed stored nothing and
// still returned nil, leaving the job-health record green while suggest and
// correction went on serving stale vocabulary.
func TestVocabularyRefresh_AllProvidersFail(t *testing.T) {
	store := &fakeVocabularyStore{}
	charts := []fakeChartProvider{
		{name: domain.ProviderDeezer, err: errors.New("error 1")},
		{name: domain.ProviderLastFM, err: errors.New("error 2")},
	}
	svc := newTestRefreshService(charts, store)

	err := svc.RunOnce(context.Background())
	if err == nil {
		t.Fatal("RunOnce = nil, want a failure naming the providers that broke")
	}
	for _, provider := range []domain.ProviderName{domain.ProviderDeezer, domain.ProviderLastFM} {
		if !strings.Contains(err.Error(), provider.String()) {
			t.Errorf("error %q does not name provider %q", err, provider)
		}
	}
	if len(store.bulkAdded) != 0 {
		t.Fatalf("got %d entries, want 0", len(store.bulkAdded))
	}
}

func TestVocabularyRefresh_ChartFailureWarnsWithTheProviderThatFailed(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	leaky := &url.Error{
		Op:  "Get",
		URL: "https://ws.audioscrobbler.com/2.0/?method=chart.gettoptracks&api_key=" + secret,
		Err: errors.New("connection refused"),
	}
	charts := []fakeChartProvider{
		{name: domain.ProviderDeezer, entries: []domain.VocabularyEntry{{Term: "Drake", Kind: "artist"}}},
		{name: domain.ProviderLastFM, err: leaky},
	}
	svc := newTestRefreshService(charts, &fakeVocabularyStore{})
	buf := captureProductionLogs(t)

	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	recs := chartFetchFailureRecords(t, buf)
	if len(recs) != 1 {
		t.Fatalf("got %d chart failure records at Info level, want 1:\n%s", len(recs), buf)
	}
	if recs[0]["provider"] != domain.ProviderLastFM.String() {
		t.Errorf("provider = %v, want %q", recs[0]["provider"], domain.ProviderLastFM)
	}
	if errText, _ := recs[0]["error"].(string); !strings.Contains(errText, "connection refused") {
		t.Errorf("error = %q, want the provider's failure", errText)
	}
	if strings.Contains(buf.String(), secret) {
		t.Errorf("provider credential leaked into logs:\n%s", buf)
	}
}

func chartFetchFailureRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unparseable log line %q: %v", line, err)
		}
		if rec["msg"] == "chart fetch failed" {
			out = append(out, rec)
		}
	}
	return out
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
	if len(store.bulkAdded) != 2 {
		t.Fatalf("got %d entries, want 2", len(store.bulkAdded))
	}
}

func newTestRefreshService(
	charts []fakeChartProvider,
	store *fakeVocabularyStore,
) *VocabularyRefreshService {
	providers := make([]ports.ChartProvider, len(charts))
	for i := range charts {
		providers[i] = &charts[i]
	}
	return NewVocabularyRefreshService(
		providers, store, 50,
	)
}

func assertEntryTerm(t *testing.T, entry domain.VocabularyEntry, want string) {
	t.Helper()
	if entry.Term != want {
		t.Errorf("got term %q, want %q", entry.Term, want)
	}
}
