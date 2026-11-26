package vocabulary_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/searchtest"
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
	shown   []search.SearchResult
	entries []search.SpellingHintsEntry
}

func searchAndIngest(t *testing.T, query string, kinds map[search.ResultKind]bool, results ...search.SearchResult) ingestedSearch {
	t.Helper()
	bg := searchtest.NewTracker(t)
	var mu sync.Mutex
	var entries []search.SpellingHintsEntry
	store := &search.FakeVocabularyStore{AddFn: func(e search.SpellingHintsEntry) error {
		mu.Lock()
		defer mu.Unlock()
		entries = append(entries, e)
		return nil
	}}
	p := &search.FakeProvider{Provider: search.ProviderDeezer, Results: results}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(), func(c *search.Collaborators) { c.Vocabulary = store })
	q, err := search.NewSearchQuery(query, kinds, everyResultOnOnePage)
	if err != nil {
		t.Fatalf("NewSearchQuery: %v", err)
	}
	out, err := svc.Execute(context.Background(), search.NewUser(), q)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	bg.Wait()
	mu.Lock()
	defer mu.Unlock()
	return ingestedSearch{shown: out.Results, entries: entries}
}

func deezerResult(kind search.ResultKind, title, subtitle string, popularity float64) search.SearchResult {
	return search.SearchResult{
		Kind:       kind,
		Title:      title,
		Subtitle:   subtitle,
		Popularity: popularity,
		Sources:    []search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: title, URL: "https://x/" + title}},
	}
}

func allKinds() map[search.ResultKind]bool {
	return map[search.ResultKind]bool{
		search.ResultKindArtist: true,
		search.ResultKindTrack:  true,
		search.ResultKindAlbum:  true,
	}
}

func entryFor(t *testing.T, entries []search.SpellingHintsEntry, term string) search.SpellingHintsEntry {
	t.Helper()
	for _, e := range entries {
		if e.Term == term {
			return e
		}
	}
	t.Fatalf("no vocabulary entry for %q in %+v", term, entries)
	return search.SpellingHintsEntry{}
}

func TestSearchIngest_MapsEachShownResultToVocabularyEntries(t *testing.T) {
	got := searchAndIngest(t, "drake", allKinds(),
		deezerResult(search.ResultKindArtist, "Drake", "", 90),
		search.DeezerTrack("Hotline Bling", "Drake", 80),
		search.DeezerAlbum("Scorpion", "Drake", 70),
	)

	if len(got.entries) != 4 {
		t.Fatalf("want 4 entries, got %d: %+v", len(got.entries), got.entries)
	}
	want := []search.SpellingHintsEntry{
		{Term: "Drake", TermNorm: "drake", Kind: search.VocabKindArtist, Popularity: 90},
		{Term: "Hotline Bling - Drake", TermNorm: "hotline bling drake", Kind: search.VocabKindTrack, Popularity: 80},
		{Term: "Drake", TermNorm: "drake", Kind: search.VocabKindArtist, Popularity: 80},
		{Term: "Scorpion - Drake", TermNorm: "scorpion drake", Kind: search.VocabKindAlbum, Popularity: 70},
	}
	for _, w := range want {
		if !slices.Contains(got.entries, w) {
			t.Errorf("missing entry %+v in %+v", w, got.entries)
		}
	}
	for _, e := range got.entries {
		if e.Kind == search.VocabKindQuery || e.Term == "drake" {
			t.Errorf("the raw query must not be ingested, got %+v", e)
		}
	}
}

func TestSearchIngest_AResultOfAnotherKindIsIngestedAsAQuery(t *testing.T) {
	got := searchAndIngest(t, "chill mix", map[search.ResultKind]bool{search.ResultKindPlaylist: true},
		deezerResult(search.ResultKindPlaylist, "Chill Mix", "", 0),
	)

	if e := entryFor(t, got.entries, "Chill Mix"); e.Kind != search.VocabKindQuery {
		t.Errorf("playlist entry kind = %q, want %q", e.Kind, search.VocabKindQuery)
	}
}

func TestSearchIngest_IngestsOnlyTheTopShownResults(t *testing.T) {
	var results []search.SearchResult
	for _, name := range []string{"Drake A", "Drake B", "Drake C", "Drake D", "Drake E", "Drake F", "Drake G"} {
		results = append(results, deezerResult(search.ResultKindArtist, name, "", 0))
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
	untitled := search.SearchResult{
		Kind:    search.ResultKindTrack,
		Sources: []search.ProviderLink{{Provider: search.ProviderDeezer, ExternalID: "untitled", URL: "https://x/untitled"}},
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
		deezerResult(search.ResultKindArtist, "Drake", "", 0),
	)

	if len(got.entries) != 1 {
		t.Fatalf("want 1 entry, got %d: %+v", len(got.entries), got.entries)
	}
}

func searchWithVocabularyStore(t *testing.T, store *search.FakeVocabularyStore) {
	t.Helper()
	bg := searchtest.NewTracker(t)
	p := &search.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{
		deezerResult(search.ResultKindArtist, "Drake", "", 90),
	}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(), func(c *search.Collaborators) { c.Vocabulary = store })
	q, err := search.NewSearchQuery("drake", allKinds(), everyResultOnOnePage)
	if err != nil {
		t.Fatalf("NewSearchQuery: %v", err)
	}
	if _, err := svc.Execute(context.Background(), search.NewUser(), q); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	bg.Wait()
}

func TestSearchIngest_AFailedAddIsLoggedAndASuccessfulOneIsNot(t *testing.T) {
	const failed = `"msg":"search.v2.vocab_ingest_failed"`

	t.Run("failing store", func(t *testing.T) {
		logs := searchtest.CaptureLogs(t)
		searchWithVocabularyStore(t, &search.FakeVocabularyStore{AddFn: func(search.SpellingHintsEntry) error { return errors.New("store down") }})

		if !strings.Contains(logs.String(), failed) {
			t.Errorf("logs = %s, want a vocab_ingest_failed warning", logs)
		}
	})
	t.Run("working store", func(t *testing.T) {
		logs := searchtest.CaptureLogs(t)
		searchWithVocabularyStore(t, &search.FakeVocabularyStore{AddFn: func(search.SpellingHintsEntry) error { return nil }})

		if strings.Contains(logs.String(), failed) {
			t.Errorf("logs = %s, want no vocab_ingest_failed warning", logs)
		}
	})
}
