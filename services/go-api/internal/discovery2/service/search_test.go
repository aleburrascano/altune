package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	legacy "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

type fakeProvider struct {
	name    domain.ProviderName
	results []domain.SearchResult
	err     error
	delay   time.Duration
}

func (p *fakeProvider) Name() domain.ProviderName { return p.name }

func (p *fakeProvider) Search(ctx context.Context, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	if p.delay > 0 {
		select {
		case <-time.After(p.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return p.results, p.err
}

func (p *fakeProvider) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

// fakeVocab reports a single known artist, so DetectIntent only splits the query
// at that exact artist boundary.
type fakeVocab struct{ artist string }

func (v fakeVocab) Add(context.Context, domain.VocabularyEntry) error       { return nil }
func (v fakeVocab) BulkAdd(context.Context, []domain.VocabularyEntry) error { return nil }
func (v fakeVocab) SuggestByPrefix(_ context.Context, prefix string, _ int) ([]domain.VocabularyEntry, error) {
	if prefix == v.artist {
		return []domain.VocabularyEntry{{Term: v.artist, Kind: domain.VocabKindArtist, Popularity: 80}}, nil
	}
	return nil, nil
}
func (v fakeVocab) FindClosest(context.Context, string, int) ([]domain.VocabularyEntry, error) {
	return nil, nil
}

func newQuery(t *testing.T, raw string) *domain.SearchQuery {
	t.Helper()
	q, err := domain.NewSearchQuery(raw, "", map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}, 20)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func newUser() shared.UserId {
	return shared.NewUserId(uuid.New())
}

func runSearch(t *testing.T, svc *Service, raw string) *legacy.SearchOutput {
	t.Helper()
	out, err := svc.Execute(context.Background(), newUser(), newQuery(t, raw), false)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return out
}

func TestService_EndToEnd_MergesAndRanks(t *testing.T) {
	// Same ISRC across two providers must merge to one entity with two sources;
	// the more popular track outranks the same-named album (bare query).
	trackP1 := withPop(track("HUMBLE.", "Kendrick Lamar", domain.ProviderDeezer, map[string]any{"isrc": "X"}), 90)
	trackP2 := withPop(track("Humble", "Kendrick Lamar", domain.ProviderITunes, map[string]any{"isrc": "X"}), 90)
	album := deezerAlbum("Humble", "Kendrick Lamar", 40)

	p1 := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{trackP1, album}}
	p2 := &fakeProvider{name: domain.ProviderITunes, results: []domain.SearchResult{trackP2}}

	svc := NewService([]ports.SearchProvider{p1, p2}, legacy.NewCircuitBreaker())
	out := runSearch(t, svc, "humble")

	if len(out.Results) != 2 {
		t.Fatalf("want 2 results, got %d: %v", len(out.Results), titles(out.Results))
	}
	if out.Results[0].Kind != domain.ResultKindTrack {
		t.Errorf("want track first, got %v", titles(out.Results))
	}
	if got := len(out.Results[0].Sources); got != 2 {
		t.Errorf("want merged track with 2 sources, got %d", got)
	}
	if out.Partial {
		t.Error("want partial=false (all providers ok)")
	}
}

func TestService_PartialOnProviderError(t *testing.T) {
	good := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	bad := &fakeProvider{name: domain.ProviderITunes, err: errors.New("boom")}

	svc := NewService([]ports.SearchProvider{good, bad}, legacy.NewCircuitBreaker())
	out := runSearch(t, svc, "humble")

	if !out.Partial {
		t.Error("want partial=true when a provider fails")
	}
	if len(out.Results) != 1 {
		t.Fatalf("want the good provider's result to survive, got %v", titles(out.Results))
	}
}

func TestService_StructuredIntentSeatsTrackAboveAlbum(t *testing.T) {
	// Pattern A end-to-end: with intent detected (artist+title → kind track),
	// the exact track is T1 even though the same-named album is far more popular.
	trk := deezerTrack("HUMBLE.", "Kendrick Lamar", 40)
	album := deezerAlbum("Humble", "Kendrick Lamar", 99)
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{album, trk}}

	svc := NewService(
		[]ports.SearchProvider{p},
		legacy.NewCircuitBreaker(),
		WithVocabularyStore(fakeVocab{artist: "kendrick lamar"}),
	)
	out := runSearch(t, svc, "kendrick lamar humble")

	if len(out.Results) == 0 || out.Results[0].Kind != domain.ResultKindTrack {
		t.Fatalf("intent should seat the track at T1, got %v", titles(out.Results))
	}
}

func TestService_LimitTruncates(t *testing.T) {
	var results []domain.SearchResult
	for i := 0; i < 5; i++ {
		results = append(results, deezerTrack("Song", "Artist "+string(rune('A'+i)), float64(50-i)))
	}
	p := &fakeProvider{name: domain.ProviderDeezer, results: results}
	svc := NewService([]ports.SearchProvider{p}, legacy.NewCircuitBreaker())

	q, err := domain.NewSearchQuery("song", "", map[domain.ResultKind]bool{domain.ResultKindTrack: true}, 3)
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), q, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 3 {
		t.Fatalf("want limit=3 enforced, got %d", len(out.Results))
	}
}
