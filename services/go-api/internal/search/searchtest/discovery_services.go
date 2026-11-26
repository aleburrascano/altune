package searchtest

import (
	"altune/go-api/internal/search/adapters/handler"
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/service/providerguard"
	"altune/go-api/internal/search/service/vocabulary"
	"testing"
)

func CompleteServices(t testing.TB, given handler.DiscoveryServices) handler.DiscoveryServices {
	t.Helper()
	s := given
	if s.Search == nil {
		s.Search = NewSearchService(t, NewTracker(t), nil, providerguard.NewCircuitBreaker())
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
		s.Suggest = vocabulary.NewSuggestService(&memory.FakeVocabularyStore{})
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
