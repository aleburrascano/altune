package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	discdomain "altune/go-api/internal/discovery/domain"

	"github.com/go-chi/chi/v5"
)

// countingSearchProvider records every fan-out call it receives.
type countingSearchProvider struct {
	fakeSearchProvider
	calls atomic.Int32
}

func (p *countingSearchProvider) Search(ctx context.Context, q string, k map[discdomain.ResultKind]bool) ([]discdomain.SearchResult, error) {
	p.calls.Add(1)
	return p.fakeSearchProvider.Search(ctx, q, k)
}

// recordingVocabStore records every term written to the shared vocabulary.
type recordingVocabStore struct {
	fakeVocabStore
	mu    sync.Mutex
	terms []string
}

func (s *recordingVocabStore) Add(_ context.Context, e discdomain.VocabularyEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.terms = append(s.terms, e.Term)
	return nil
}

func (s *recordingVocabStore) Trim(context.Context, int) error { return nil }

func (s *recordingVocabStore) written() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.terms...)
}

func buildQueryCapRouter(p ports.SearchProvider, vocab ports.VocabularyStore) chi.Router {
	svc := service.NewService([]ports.SearchProvider{p}, service.NewCircuitBreaker(), service.WithVocabularyStore(vocab))
	h := NewDiscoveryHandler(DiscoveryServices{Search: svc})
	r := chi.NewRouter()
	r.Use(auth.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
}

func newQueryCapFixture() (*countingSearchProvider, *recordingVocabStore, chi.Router) {
	p := &countingSearchProvider{fakeSearchProvider: fakeSearchProvider{
		name: discdomain.ProviderDeezer,
		results: []discdomain.SearchResult{{
			Kind: discdomain.ResultKindTrack, Title: "Song", Subtitle: "Artist",
			Confidence: discdomain.ConfidenceLow,
			Sources: []discdomain.SourceRef{
				{Provider: discdomain.ProviderDeezer, ExternalID: "1", URL: "https://deezer.com/1"},
			},
		}},
	}}
	vocab := &recordingVocabStore{}
	return p, vocab, buildQueryCapRouter(p, vocab)
}

func wordQuery(n int) string {
	return strings.TrimSpace(strings.Repeat("a ", n))
}

// TestHandleSearch_HighTokenQuery_RejectedBeforeFanOutAndVocab guards #1087: a
// query under the rune cap but over the token cap must never reach provider
// fan-out, correction, or the shared vocabulary index.
func TestHandleSearch_HighTokenQuery_RejectedBeforeFanOutAndVocab(t *testing.T) {
	p, vocab, router := newQueryCapFixture()
	raw := wordQuery(discdomain.MaxSearchQueryTokens + 1)
	if len([]rune(raw)) > discdomain.MaxSearchQueryRunes {
		t.Fatalf("fixture must stay under the rune cap to isolate the token cap")
	}

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q="+url.QueryEscape(raw), nil)

	discAssertStatus(t, rec, http.StatusBadRequest)
	time.Sleep(100 * time.Millisecond) // let any stray background ingest land
	if n := p.calls.Load(); n != 0 {
		t.Errorf("provider fan-out calls = %d, want 0", n)
	}
	if terms := vocab.written(); len(terms) != 0 {
		t.Errorf("vocabulary writes = %q, want none", terms)
	}
}

func TestHandleSearch_QueryAtTokenCap_StillSearchesAndIngests(t *testing.T) {
	p, vocab, router := newQueryCapFixture()
	raw := wordQuery(discdomain.MaxSearchQueryTokens)

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q="+url.QueryEscape(raw), nil)

	discAssertStatus(t, rec, http.StatusOK)
	if p.calls.Load() == 0 {
		t.Fatalf("provider was not called for a query at the token cap")
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(vocab.written()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	terms := vocab.written()
	want := []string{"Song - Artist", "Artist"}
	if len(terms) != len(want) {
		t.Fatalf("vocabulary writes = %q, want only provider-derived %q", terms, want)
	}
	for i := range want {
		if terms[i] != want[i] {
			t.Errorf("vocabulary writes = %q, want only provider-derived %q", terms, want)
			break
		}
	}
	for _, term := range terms {
		if term == raw {
			t.Errorf("raw query %q must not be ingested into the vocabulary", raw)
		}
	}
}
