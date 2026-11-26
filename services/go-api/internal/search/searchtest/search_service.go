package searchtest

import (
	"altune/go-api/internal/search/internal/core/history"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/service/artwork"
	"altune/go-api/internal/search/internal/service/providerguard"
	"altune/go-api/internal/search/internal/service/query"
	"altune/go-api/internal/search/internal/service/vocabulary"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"testing"
)

func NewSearchService(t testing.TB, bg *background.Tracker, providers []ports.SearchProvider, breaker *providerguard.CircuitBreaker, opts ...query.Option) *query.Service {
	t.Helper()
	return NewSearchServiceWith(t, bg, providers, breaker, func(*query.Collaborators) {}, opts...)
}

func NewSearchServiceWith(t testing.TB, bg *background.Tracker, providers []ports.SearchProvider, breaker *providerguard.CircuitBreaker, with func(*query.Collaborators), opts ...query.Option) *query.Service {
	t.Helper()
	collaborators := query.AbsentCollaborators()
	with(&collaborators)
	svc, err := query.NewService(bg, providers, breaker, collaborators, opts...)
	if err != nil {
		t.Fatalf("query.NewService: %v", err)
	}
	return svc
}

type artworkParts struct {
	lookup     ports.TaggingArtworkLookup
	cache      ports.ArtworkCache
	identities ports.IdentityStore
	mbidIndex  ports.MBIDIndex
}

type ArtworkPart func(*artworkParts)

func UseArtworkLookup(l ports.TaggingArtworkLookup) ArtworkPart {
	return func(p *artworkParts) { p.lookup = l }
}

func UseArtworkCache(c ports.ArtworkCache) ArtworkPart {
	return func(p *artworkParts) { p.cache = c }
}

func UseIdentityStore(s ports.IdentityStore) ArtworkPart {
	return func(p *artworkParts) { p.identities = s }
}

func UseMBIDIndex(i ports.MBIDIndex) ArtworkPart {
	return func(p *artworkParts) { p.mbidIndex = i }
}

func NewArtworkFill(t testing.TB, bg *background.Tracker, parts ...ArtworkPart) *artwork.ArtworkFiller {
	t.Helper()
	p := artworkParts{
		lookup:     artwork.NoLookup{},
		identities: artwork.NoIdentities{},
		mbidIndex:  artwork.NoMBIDIndex{},
	}
	for _, part := range parts {
		part(&p)
	}
	if p.cache == nil {
		pending, err := artwork.NewPendingFiller(p.identities, bg)
		if err != nil {
			t.Fatalf("artwork.NewPendingFiller: %v", err)
		}
		return pending
	}
	filler, err := artwork.NewArtworkFiller(p.lookup, p.cache, p.identities, p.mbidIndex, bg)
	if err != nil {
		t.Fatalf("artwork.NewArtworkFiller: %v", err)
	}
	return filler
}

func NewCorrection(t testing.TB, vocab ports.SpellingHintsReader) *vocabulary.CorrectionService {
	t.Helper()
	svc, err := vocabulary.NewCorrectionService(vocab)
	if err != nil {
		t.Fatalf("vocabulary.NewCorrectionService: %v", err)
	}
	return svc
}

func NewHistoryRecorder(t testing.TB, repo ports.HistoryWriter) *history.RecordSearchHistoryService {
	t.Helper()
	svc, err := history.NewRecordSearchHistoryService(repo, clock.Now)
	if err != nil {
		t.Fatalf("history.NewRecordSearchHistoryService: %v", err)
	}
	return svc
}
