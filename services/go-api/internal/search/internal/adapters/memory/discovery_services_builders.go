package memory

import (
	"altune/go-api/internal/search/internal/adapters/handler"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/clock"
)

func CompleteServices(t TB, given handler.DiscoveryServices) handler.DiscoveryServices {
	t.Helper()
	s := given
	if s.Search == nil {
		s.Search = NewSearchService(t, NewTracker(t), nil, service.NewCircuitBreaker(clock.Now))
	}
	if s.History == nil {
		s.History = NewListHistory(t, &FakeHistoryReader{})
	}
	if s.RecordHistory == nil {
		s.RecordHistory = NewHistoryRecorder(t, &FakeHistoryWriter{})
	}
	if s.ClearHistory == nil {
		s.ClearHistory = NewClearHistory(t, &FakeHistoryEraser{})
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
		s.Suggest = service.NewSuggestService(&FakeVocabularyStore{})
	}
	if s.Event == nil {
		s.Event = NewRecordEvent(t, &FakeEventStore{})
	}
	if s.Favorites == nil {
		s.Favorites = NewFavorites(t, &FakeFavoritesRepo{})
	}
	if s.Unreleased == nil {
		s.Unreleased = handler.NoUnreleased{}
	}
	if s.Enrichers.Deezer == nil {
		s.Enrichers.Deezer = NewDeezerEnrichment(t, &DeezerEnricher{}, NewNameKeyedCache[domain.DeezerEnrichment]())
	}
	if s.Enrichers.Lyrics == nil {
		s.Enrichers.Lyrics = NewLyrics(t, &LyricsProvider{}, NewNameKeyedCache[domain.DeezerLyrics]())
	}
	if s.Ownership == nil {
		s.Ownership = handler.NoOwnership{}
	}
	if s.ClientEvents == nil {
		s.ClientEvents = handler.NoClientEvents{}
	}
	s.RateLimits = withDefaultLimits(s.RateLimits)
	return s
}

func withDefaultLimits(given handler.DiscoveryRateLimits) handler.DiscoveryRateLimits {
	defaults := handler.DefaultDiscoveryRateLimits
	for _, pair := range []struct{ got, def *handler.RequestLimit }{
		{&given.Search, &defaults.Search},
		{&given.Suggest, &defaults.Suggest},
		{&given.Events, &defaults.Events},
		{&given.Content, &defaults.Content},
		{&given.Favorites, &defaults.Favorites},
		{&given.History, &defaults.History},
	} {
		if *pair.got == (handler.RequestLimit{}) {
			*pair.got = *pair.def
		}
	}
	return given
}
