package vocabulary_test

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/discovery/discoverytest"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
)

const (
	vocabIngestTop       = 5
	everyResultOnOnePage = 50
)

type ingestedSearch struct {
	shown   []discovery.SearchResult
	entries []discovery.VocabularyEntry
}

func searchAndIngest(t *testing.T, query string, kinds map[discovery.ResultKind]bool, results ...discovery.SearchResult) ingestedSearch {
	t.Helper()
	bg := discoverytest.NewTracker(t)
	var mu sync.Mutex
	var entries []discovery.VocabularyEntry
	store := &discovery.FakeVocabularyStore{AddFn: func(e discovery.VocabularyEntry) error {
		mu.Lock()
		defer mu.Unlock()
		entries = append(entries, e)
		return nil
	}}
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: results}
	svc := discovery.NewSearchService(bg, []discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithVocabularyStore(store))
	q, err := discovery.NewSearchQuery(query, kinds, everyResultOnOnePage)
	if err != nil {
		t.Fatalf("NewSearchQuery: %v", err)
	}
	out, err := svc.Execute(context.Background(), discovery.NewUser(), q, false)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	bg.Wait()
	mu.Lock()
	defer mu.Unlock()
	return ingestedSearch{shown: out.Results, entries: entries}
}

func deezerResult(kind discovery.ResultKind, title, subtitle string, popularity float64) discovery.SearchResult {
	return discovery.SearchResult{
		Kind:       kind,
		Title:      title,
		Subtitle:   subtitle,
		Popularity: popularity,
		Sources:    []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: title, URL: "https://x/" + title}},
	}
}

func allKinds() map[discovery.ResultKind]bool {
	return map[discovery.ResultKind]bool{
		discovery.ResultKindArtist: true,
		discovery.ResultKindTrack:  true,
		discovery.ResultKindAlbum:  true,
	}
}

func entryFor(t *testing.T, entries []discovery.VocabularyEntry, term string) discovery.VocabularyEntry {
	t.Helper()
	for _, e := range entries {
		if e.Term == term {
			return e
		}
	}
	t.Fatalf("no vocabulary entry for %q in %+v", term, entries)
	return discovery.VocabularyEntry{}
}

func TestSearchIngest_MapsEachShownResultToVocabularyEntries(t *testing.T) {
	got := searchAndIngest(t, "drake", allKinds(),
		deezerResult(discovery.ResultKindArtist, "Drake", "", 90),
		discovery.DeezerTrack("Hotline Bling", "Drake", 80),
		discovery.DeezerAlbum("Scorpion", "Drake", 70),
	)

	if len(got.entries) != 4 {
		t.Fatalf("want 4 entries, got %d: %+v", len(got.entries), got.entries)
	}
	want := []discovery.VocabularyEntry{
		{Term: "Drake", TermNorm: "drake", Kind: discovery.VocabKindArtist, Popularity: 90},
		{Term: "Hotline Bling - Drake", TermNorm: "hotline bling drake", Kind: discovery.VocabKindTrack, Popularity: 80},
		{Term: "Drake", TermNorm: "drake", Kind: discovery.VocabKindArtist, Popularity: 80},
		{Term: "Scorpion - Drake", TermNorm: "scorpion drake", Kind: discovery.VocabKindAlbum, Popularity: 70},
	}
	for _, w := range want {
		if !slices.Contains(got.entries, w) {
			t.Errorf("missing entry %+v in %+v", w, got.entries)
		}
	}
	for _, e := range got.entries {
		if e.Kind == discovery.VocabKindQuery || e.Term == "drake" {
			t.Errorf("the raw query must not be ingested, got %+v", e)
		}
	}
}

func TestSearchIngest_AResultOfAnotherKindIsIngestedAsAQuery(t *testing.T) {
	got := searchAndIngest(t, "chill mix", map[discovery.ResultKind]bool{discovery.ResultKindPlaylist: true},
		deezerResult(discovery.ResultKindPlaylist, "Chill Mix", "", 0),
	)

	if e := entryFor(t, got.entries, "Chill Mix"); e.Kind != discovery.VocabKindQuery {
		t.Errorf("playlist entry kind = %q, want %q", e.Kind, discovery.VocabKindQuery)
	}
}

func TestSearchIngest_IngestsOnlyTheTopShownResults(t *testing.T) {
	var results []discovery.SearchResult
	for _, name := range []string{"Drake A", "Drake B", "Drake C", "Drake D", "Drake E", "Drake F", "Drake G"} {
		results = append(results, deezerResult(discovery.ResultKindArtist, name, "", 0))
	}

	got := searchAndIngest(t, "drake", allKinds(), results...)

	if len(got.shown) <= vocabIngestTop {
		t.Fatalf("precondition: want more than %d shown results, got %d", vocabIngestTop, len(got.shown))
	}
	if len(got.entries) != vocabIngestTop {
		t.Fatalf("want %d entries, got %d: %+v", vocabIngestTop, len(got.entries), got.entries)
	}
	for i, e := range got.entries {
		if e.Term != got.shown[i].Title {
			t.Errorf("entry %d = %q, want the shown result %q", i, e.Term, got.shown[i].Title)
		}
	}
}

func TestSearchIngest_AnEmptyTitleIsIngestedAsAnEmptyTerm(t *testing.T) {
	untitled := discovery.SearchResult{
		Kind:    discovery.ResultKindTrack,
		Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "untitled", URL: "https://x/untitled"}},
	}
	queryWithNoRankableWord := "a"

	got := searchAndIngest(t, queryWithNoRankableWord, allKinds(), untitled)

	if len(got.entries) != 1 {
		t.Fatalf("want 1 entry, got %d: %+v", len(got.entries), got.entries)
	}
	if got.entries[0].Term != "" || got.entries[0].TermNorm != "" {
		t.Errorf("want the empty term ingested as-is, got %+v", got.entries[0])
	}
}

func TestSearchIngest_FewerResultsThanTheTopAreAllIngested(t *testing.T) {
	got := searchAndIngest(t, "drake", allKinds(),
		deezerResult(discovery.ResultKindArtist, "Drake", "", 0),
	)

	if len(got.entries) != 1 {
		t.Fatalf("want 1 entry, got %d: %+v", len(got.entries), got.entries)
	}
}

func searchWithVocabularyStore(t *testing.T, store *discovery.FakeVocabularyStore) {
	t.Helper()
	bg := discoverytest.NewTracker(t)
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{
		deezerResult(discovery.ResultKindArtist, "Drake", "", 90),
	}}
	svc := discovery.NewSearchService(bg, []discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithVocabularyStore(store))
	q, err := discovery.NewSearchQuery("drake", allKinds(), everyResultOnOnePage)
	if err != nil {
		t.Fatalf("NewSearchQuery: %v", err)
	}
	if _, err := svc.Execute(context.Background(), discovery.NewUser(), q, false); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	bg.Wait()
}

func TestSearchIngest_AFailedAddIsLoggedAndASuccessfulOneIsNot(t *testing.T) {
	const failed = `"msg":"search.v2.vocab_ingest_failed"`

	t.Run("failing store", func(t *testing.T) {
		logs := discoverytest.CaptureLogs(t)
		searchWithVocabularyStore(t, &discovery.FakeVocabularyStore{AddFn: func(discovery.VocabularyEntry) error { return errors.New("store down") }})

		if !strings.Contains(logs.String(), failed) {
			t.Errorf("logs = %s, want a vocab_ingest_failed warning", logs)
		}
	})
	t.Run("working store", func(t *testing.T) {
		logs := discoverytest.CaptureLogs(t)
		searchWithVocabularyStore(t, &discovery.FakeVocabularyStore{AddFn: func(discovery.VocabularyEntry) error { return nil }})

		if strings.Contains(logs.String(), failed) {
			t.Errorf("logs = %s, want no vocab_ingest_failed warning", logs)
		}
	})
}
