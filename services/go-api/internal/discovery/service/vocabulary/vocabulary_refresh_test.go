package vocabulary_test

import (
	"altune/go-api/internal/discovery"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
)

type fakeChartProvider struct {
	name    discovery.ProviderName
	entries []discovery.VocabularyEntry
	err     error
}

func (f *fakeChartProvider) Name() discovery.ProviderName { return f.name }

func (f *fakeChartProvider) FetchCharts(_ context.Context, _ int) ([]discovery.VocabularyEntry, error) {
	return f.entries, f.err
}

func TestVocabularyRefresh_HappyPath(t *testing.T) {
	store := &discovery.FakeVocabularyStore{}
	charts := []fakeChartProvider{
		{entries: []discovery.VocabularyEntry{
			{Term: "Drake", Kind: "artist", Popularity: 1000},
		}},
		{entries: []discovery.VocabularyEntry{
			{Term: "Blinding Lights", Kind: "track", Popularity: 500},
		}},
	}
	svc := newTestRefreshService(charts, store)

	err := svc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.BulkAdded) != 2 {
		t.Fatalf("got %d entries, want 2", len(store.BulkAdded))
	}
	assertEntryTerm(t, store.BulkAdded[0], "Drake")
	assertEntryTerm(t, store.BulkAdded[1], "Blinding Lights")
}

func TestVocabularyRefresh_NormalizesTerms(t *testing.T) {
	store := &discovery.FakeVocabularyStore{}
	charts := []fakeChartProvider{
		{entries: []discovery.VocabularyEntry{
			{Term: "The Weeknd", Kind: "artist", Popularity: 900},
		}},
	}
	svc := newTestRefreshService(charts, store)

	err := svc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.BulkAdded) != 1 {
		t.Fatalf("got %d entries, want 1", len(store.BulkAdded))
	}
	if store.BulkAdded[0].TermNorm == "" {
		t.Error("TermNorm should be set after normalization")
	}
	if store.BulkAdded[0].TermNorm == store.BulkAdded[0].Term {
		t.Error("TermNorm should differ from Term for 'The Weeknd'")
	}
}

func TestVocabularyRefresh_OneProviderFails(t *testing.T) {
	store := &discovery.FakeVocabularyStore{}
	charts := []fakeChartProvider{
		{name: discovery.ProviderLastFM, err: errors.New("network timeout")},
		{name: discovery.ProviderDeezer, entries: []discovery.VocabularyEntry{
			{Term: "Bad Bunny", Kind: "artist", Popularity: 800},
		}},
	}
	svc := newTestRefreshService(charts, store)

	err := svc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.BulkAdded) != 1 {
		t.Fatalf("got %d entries, want 1", len(store.BulkAdded))
	}
	assertEntryTerm(t, store.BulkAdded[0], "Bad Bunny")
}

func TestVocabularyRefresh_AllProvidersFail(t *testing.T) {
	store := &discovery.FakeVocabularyStore{}
	charts := []fakeChartProvider{
		{name: discovery.ProviderDeezer, err: errors.New("error 1")},
		{name: discovery.ProviderLastFM, err: errors.New("error 2")},
	}
	svc := newTestRefreshService(charts, store)

	err := svc.RunOnce(context.Background())
	if err == nil {
		t.Fatal("RunOnce = nil, want a failure naming the providers that broke")
	}
	for _, provider := range []discovery.ProviderName{discovery.ProviderDeezer, discovery.ProviderLastFM} {
		if !strings.Contains(err.Error(), provider.String()) {
			t.Errorf("error %q does not name provider %q", err, provider)
		}
	}
	if len(store.BulkAdded) != 0 {
		t.Fatalf("got %d entries, want 0", len(store.BulkAdded))
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
		{name: discovery.ProviderDeezer, entries: []discovery.VocabularyEntry{{Term: "Drake", Kind: "artist"}}},
		{name: discovery.ProviderLastFM, err: leaky},
	}
	svc := newTestRefreshService(charts, &discovery.FakeVocabularyStore{})
	buf := discovery.CaptureProductionLogs(t)

	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	recs := chartFetchFailureRecords(t, buf)
	if len(recs) != 1 {
		t.Fatalf("got %d chart failure records at Info level, want 1:\n%s", len(recs), buf)
	}
	if recs[0]["provider"] != discovery.ProviderLastFM.String() {
		t.Errorf("provider = %v, want %q", recs[0]["provider"], discovery.ProviderLastFM)
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
	store := &discovery.FakeVocabularyStore{}
	charts := []fakeChartProvider{
		{entries: []discovery.VocabularyEntry{}},
	}
	svc := newTestRefreshService(charts, store)

	err := svc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.BulkAdded) != 0 {
		t.Fatalf("got %d entries, want 0", len(store.BulkAdded))
	}
}

func TestVocabularyRefresh_StoreError(t *testing.T) {
	store := &discovery.FakeVocabularyStore{Err: errors.New("redis down")}
	charts := []fakeChartProvider{
		{entries: []discovery.VocabularyEntry{
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
	store := &discovery.FakeVocabularyStore{}
	charts := []fakeChartProvider{
		{entries: []discovery.VocabularyEntry{
			{Term: "Drake", Kind: "artist", Popularity: 1000},
		}},
		{entries: []discovery.VocabularyEntry{
			{Term: "Drake", Kind: "artist", Popularity: 900},
		}},
	}
	svc := newTestRefreshService(charts, store)

	err := svc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.BulkAdded) != 2 {
		t.Fatalf("got %d entries, want 2", len(store.BulkAdded))
	}
}

func newTestRefreshService(
	charts []fakeChartProvider,
	store *discovery.FakeVocabularyStore,
) *discovery.VocabularyRefreshService {
	providers := make([]discovery.ChartProvider, len(charts))
	for i := range charts {
		providers[i] = &charts[i]
	}
	return discovery.NewVocabularyRefreshService(
		providers, store, 50,
	)
}

func assertEntryTerm(t *testing.T, entry discovery.VocabularyEntry, want string) {
	t.Helper()
	if entry.Term != want {
		t.Errorf("got term %q, want %q", entry.Term, want)
	}
}
