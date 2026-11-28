package searchtest

import (
	"altune/go-api/internal/search/internal/adapters/handler"
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/clock"
	"testing"
)

func CompleteServices(t testing.TB, given handler.DiscoveryServices) handler.DiscoveryServices {
	t.Helper()
	s := given
	if s.Search == nil {
		s.Search = NewSearchService(t, NewTracker(t), nil, service.NewCircuitBreaker(clock.Now))
	}
	if s.History == nil {
		s.History = NewListHistory(t, &memory.FakeHistoryReader{})
	}
	if s.RecordHistory == nil {
		s.RecordHistory = NewHistoryRecorder(t, &memory.FakeHistoryWriter{})
	}
	if s.ClearHistory == nil {
		s.ClearHistory = NewClearHistory(t, &memory.FakeHistoryEraser{})
	}
	if s.Album == nil {
		s.Album = NewAlbumTracks(t, map[domain.ProviderName]ports.AlbumContentProvider{})
	}
	if s.Artist == nil {
		s.Artist = NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{})
	}
	if s.Related == nil {
		s.Related = NewRelatedTracks(t, map[string]ports.RelatedTracksProvider{})
	}
	if s.Enrich == nil {
		s.Enrich = handler.UnconfiguredEnrichment{}
	}
	if s.Suggest == nil {
		s.Suggest = service.NewSuggestService(&memory.FakeVocabularyStore{})
	}
	if s.Event == nil {
		s.Event = NewRecordEvent(t, &memory.FakeEventStore{})
	}
	if s.Favorites == nil {
		s.Favorites = NewFavorites(t, &memory.FakeFavoritesRepo{})
	}
	if s.Unreleased == nil {
		s.Unreleased = handler.NoUnreleased{}
	}
	if s.Enrichers.Deezer == nil {
		s.Enrichers.Deezer = NewDeezerEnrichment(t, &memory.DeezerEnricher{}, NewNameKeyedCache[domain.DeezerEnrichment]())
	}
	if s.Enrichers.Lyrics == nil {
		s.Enrichers.Lyrics = NewLyrics(t, &memory.LyricsProvider{}, NewNameKeyedCache[domain.DeezerLyrics]())
	}
	if s.Ownership == nil {
		s.Ownership = handler.NoOwnership{}
	}
	if s.ClientEvents == nil {
		s.ClientEvents = handler.NoClientEvents{}
	}
	return s
}
