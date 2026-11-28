package searchtest

import (
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"testing"
)

func NewSearchService(t testing.TB, bg *background.Tracker, providers []ports.SearchProvider, breaker *service.CircuitBreaker, opts ...service.Option) *service.SearchService {
	t.Helper()
	return NewSearchServiceWith(t, bg, providers, breaker, func(*service.Collaborators) {}, opts...)
}

func NewSearchServiceWith(t testing.TB, bg *background.Tracker, providers []ports.SearchProvider, breaker *service.CircuitBreaker, with func(*service.Collaborators), opts ...service.Option) *service.SearchService {
	t.Helper()
	collaborators := service.AbsentCollaborators()
	with(&collaborators)
	svc, err := service.NewSearchService(bg, providers, breaker, collaborators, opts...)
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

func NewArtworkFill(t testing.TB, bg *background.Tracker, parts ...ArtworkPart) *service.ArtworkFiller {
	t.Helper()
	p := artworkParts{
		lookup:     service.NoLookup{},
		identities: service.NoIdentities{},
		mbidIndex:  service.NoArtworkMBIDIndex{},
	}
	for _, part := range parts {
		part(&p)
	}
	if p.cache == nil {
		pending, err := service.NewPendingFiller(p.identities, bg)
		if err != nil {
			t.Fatalf("artwork.NewPendingFiller: %v", err)
		}
		return pending
	}
	filler, err := service.NewArtworkFiller(p.lookup, p.cache, p.identities, p.mbidIndex, bg)
	if err != nil {
		t.Fatalf("artwork.NewArtworkFiller: %v", err)
	}
	return filler
}

func NewCorrection(t testing.TB, vocab ports.SpellingHintsReader) *service.CorrectionService {
	t.Helper()
	svc, err := service.NewCorrectionService(vocab)
	if err != nil {
		t.Fatalf("vocabulary.NewCorrectionService: %v", err)
	}
	return svc
}

func NewHistoryRecorder(t testing.TB, repo ports.HistoryWriter) *service.RecordSearchHistoryService {
	t.Helper()
	svc, err := service.NewRecordSearchHistoryService(repo, clock.Now, ids.Real)
	if err != nil {
		t.Fatalf("history.NewRecordSearchHistoryService: %v", err)
	}
	return svc
}
