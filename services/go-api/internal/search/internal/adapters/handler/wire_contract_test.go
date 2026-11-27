package handler_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/handler"
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/core/details"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/jsoncontract"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func wireRouter(t *testing.T, services handler.DiscoveryServices) chi.Router {
	t.Helper()
	h := handler.MustNewDiscoveryHandler(searchtest.CompleteServices(t, services), time.Now)
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

func wireGet(t *testing.T, router chi.Router, path, fixture string) {
	t.Helper()
	rec := discServe(t, router, http.MethodGet, path, nil)
	discAssertStatus(t, rec, http.StatusOK)
	jsoncontract.AssertWire(t, fixture, rec.Body.Bytes())
}

func wireSend(t *testing.T, router chi.Router, method, path, requestFixture string, body any, want int) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	jsoncontract.AssertWire(t, requestFixture, encoded)
	rec := discServe(t, router, method, path, strings.NewReader(string(encoded)))
	discAssertStatus(t, rec, want)
	return rec
}

func wireResult(kind search.ResultKind, title, subtitle, id string) search.SearchResult {
	return domain.NewProviderResult(kind, title, subtitle, "https://img.example/"+id+".jpg",
		search.ProviderLink{Provider: search.ProviderDeezer, ExternalID: id, URL: "https://deezer.com/" + id},
		map[string]any{"duration_seconds": 215, "explicit": false})
}

func TestWire_DiscoverySearch(t *testing.T) {
	provider := &fakeSearchProvider{name: search.ProviderDeezer, results: []search.SearchResult{
		wireResult(search.ResultKindTrack, "Alright", "Kendrick Lamar", "t1"),
		wireResult(search.ResultKindAlbum, "To Pimp a Butterfly", "Kendrick Lamar", "a1"),
		wireResult(search.ResultKindArtist, "Kendrick Lamar", "", "ar1"),
	}}
	router := buildDiscoveryRouter(t, provider, &fakeSearchHistoryRepo{}, nil, nil)

	wireGet(t, router, "/discovery/search?q=kendrick", "discovery_search")
}

func TestWire_DiscoverySuggest(t *testing.T) {
	vocab := &fakeVocabStore{entries: []domain.SpellingHintsEntry{
		{Term: "Kendrick Lamar", TermNorm: "kendrick lamar", Kind: domain.VocabKindArtist, Popularity: 99},
	}}
	router := buildSuggestRouter(t, vocab)

	wireGet(t, router, "/discovery/suggest?q=kend", "discovery_suggest")
}

func TestWire_DiscoverySearchHistory(t *testing.T) {
	repo := &fakeSearchHistoryRepo{}
	router := buildDiscoveryRouter(t, nil, repo, nil, nil)

	wireSend(t, router, http.MethodPost, "/discovery/search-history", "req_discovery_search_history",
		map[string]any{"query": "kendrick lamar"}, http.StatusNoContent)
	repo.entries = []*domain.SearchHistoryEntry{{Query: "Kendrick Lamar", QueryNorm: "kendrick lamar", ExecutedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}}
	wireGet(t, router, "/discovery/search-history", "discovery_search_history")
	discAssertStatus(t, discServe(t, router, http.MethodDelete, "/discovery/search-history", nil), http.StatusNoContent)
}

func TestWire_DiscoveryFavorites(t *testing.T) {
	repo := &memory.FakeFavoritesRepo{Favorites: []domain.Favorite{
		{Kind: search.ResultKindAlbum, Key: "album|damn.|kendrick lamar", Title: "DAMN.", Subtitle: "Kendrick Lamar", ImageURL: "https://img.example/damn.jpg"},
	}}
	router := wireRouter(t, handler.DiscoveryServices{Favorites: searchtest.NewFavorites(t, repo)})
	request := map[string]any{"kind": "album", "title": "DAMN.", "subtitle": "Kendrick Lamar", "image_url": "https://img.example/damn.jpg"}

	wireGet(t, router, "/discovery/favorites", "discovery_favorites")
	rec := wireSend(t, router, http.MethodPut, "/discovery/favorites", "req_discovery_favorite", request, http.StatusOK)
	jsoncontract.AssertWire(t, "discovery_favorite_put", rec.Body.Bytes())
	discAssertStatus(t, discServe(t, router, http.MethodDelete, "/discovery/favorites", discJsonBody(t, request)), http.StatusNoContent)
}

func TestWire_DiscoveryEnrichment(t *testing.T) {
	svc := mustEnrichment(details.NewEnrichmentService(&fakeMetadataEnricher{enrichment: sampleAlbumEnrichment()}, details.NoArtwork{}, details.NoEnrichmentCache{}, details.NoMBIDIndex{}))
	router := wireRouter(t, handler.DiscoveryServices{Enrich: svc})

	wireGet(t, router, "/discovery/enrichment?kind=album&title=DAMN.&subtitle=Kendrick+Lamar", "discovery_enrichment")
}

func TestWire_DiscoveryDeezerEnrichment(t *testing.T) {
	e := domain.EmptyDeezerEnrichment()
	e.BPM = 90
	e.Label = "Top Dawg"
	e.Genres = []string{"hip hop"}
	e.UPC = "0602557"
	e.RecordType = "album"
	svc := searchtest.NewDeezerEnrichment(t, &scriptedDeezerEnricher{id: "dz-1", found: &e}, search.NoNameKeyedCache[domain.DeezerEnrichment]{})
	router := wireRouter(t, handler.DiscoveryServices{Enrichers: handler.DetailEnrichers{Deezer: svc}})

	wireGet(t, router, "/discovery/enrichment/deezer?kind=album&title=DAMN.&subtitle=Kendrick+Lamar", "discovery_enrichment_deezer")
}

func TestWire_DiscoveryLyrics(t *testing.T) {
	l := domain.EmptyDeezerLyrics()
	l.Plain = "I never sleep"
	l.SyncedLines = []domain.SyncedLyricLine{{Timecode: "[00:12.30]", Line: "I never sleep", Milliseconds: 12300, Duration: 2100}}
	l.Writers = []string{"Nasir Jones"}
	l.Copyright = "1994 Columbia"
	svc := searchtest.NewLyrics(t, &scriptedLyricsProvider{id: "t-1", found: &l}, search.NoNameKeyedCache[domain.DeezerLyrics]{})
	router := wireRouter(t, handler.DiscoveryServices{Enrichers: handler.DetailEnrichers{Lyrics: svc}})

	wireGet(t, router, "/discovery/lyrics?title=N.Y.+State+of+Mind&subtitle=Nas", "discovery_lyrics")
}

func TestWire_DiscoveryAlbumTracks(t *testing.T) {
	provider := &memory.AlbumContentProvider{Results: []search.SearchResult{
		wireResult(search.ResultKindTrack, "DNA.", "Kendrick Lamar", "t1"),
		wireResult(search.ResultKindTrack, "HUMBLE.", "Kendrick Lamar", "t2"),
	}}
	svc := searchtest.NewAlbumTracks(t, map[search.ProviderName]ports.AlbumContentProvider{search.ProviderDeezer: provider})
	router := wireRouter(t, handler.DiscoveryServices{Album: svc})

	wireGet(t, router, "/discovery/albums/deezer/d1/tracks", "discovery_album_tracks")
}

func TestWire_DiscoveryRelatedTracks(t *testing.T) {
	provider := &fakeRelatedTracksProvider{results: []search.SearchResult{
		wireResult(search.ResultKindTrack, "Similar", "Someone", "t9"),
	}}
	svc := searchtest.NewRelatedTracks(t, map[string]ports.RelatedTracksProvider{"soundcloud": provider})
	router := wireRouter(t, handler.DiscoveryServices{Related: svc})

	wireGet(t, router, "/discovery/tracks/soundcloud/1/related", "discovery_related_tracks")
}

func TestWire_DiscoveryArtistContent(t *testing.T) {
	provider := &memory.ArtistContentProvider{
		TopTracks: []search.SearchResult{wireResult(search.ResultKindTrack, "Alright", "Kendrick Lamar", "t1")},
		Albums:    []search.SearchResult{wireResult(search.ResultKindAlbum, "DAMN.", "Kendrick Lamar", "a1")},
	}
	svc := searchtest.NewArtistContent(t, map[search.ProviderName]ports.ArtistContentProvider{search.ProviderDeezer: provider})
	router := wireRouter(t, handler.DiscoveryServices{Artist: svc})

	wireGet(t, router, "/discovery/artists/deezer/1/content", "discovery_artist_content")
}

func TestWire_DiscoveryUnreleased(t *testing.T) {
	genius := &memory.GeniusSongCatalog{
		ArtistID: 7, ArtistFound: true,
		Songs: []domain.UnreleasedSong{
			{GeniusID: 2, Title: "Cancun", GeniusURL: "https://genius.com/c", CoverURL: "https://img.example/c.jpg", ReleaseDate: "2022-07-24", Quality: "lossless"},
		},
		MediaByID: map[int]bool{2: true},
	}
	router := unreleasedRouter(t, genius, nil)
	wireGet(t, router, "/discovery/artists/deezer/1/unreleased?name=Playboi+Carti", "discovery_artist_unreleased")

	store := &searchtest.UnreleasedStore{Hits: []ports.UnreleasedHit{
		{Artist: ports.UnreleasedArtistKey{Provider: search.ProviderDeezer, ExternalID: "42"}, ArtistName: "Playboi Carti", Song: domain.UnreleasedSong{GeniusID: 1, Title: "Cancun", Quality: "lossless"}},
	}}
	wireGet(t, unreleasedSearchRouter(t, store), "/discovery/unreleased/search?q=cancun", "discovery_unreleased_search")
}

func interactionTypeNames(accept func(domain.InteractionType) bool) []string {
	var names []string
	for e := domain.InteractionType(1); e < 128; e++ {
		if e.String() != "unknown" && accept(e) {
			names = append(names, e.String())
		}
	}
	sort.Strings(names)
	return names
}

func marshalWire(t *testing.T, v any) []byte {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return out
}

func TestWire_DiscoveryEventTypes(t *testing.T) {
	all := interactionTypeNames(func(domain.InteractionType) bool { return true })
	submittable := interactionTypeNames(domain.InteractionType.ClientSubmittable)

	jsoncontract.AssertWireExact(t, "discovery_event_types", marshalWire(t, all))
	jsoncontract.AssertWireExact(t, "discovery_event_types_client_submittable", marshalWire(t, submittable))
}

func TestWire_DiscoveryEventRequest(t *testing.T) {
	store := &recordingEventStore{}
	router := buildEventRouter(t, store)
	request := map[string]any{
		"type":               "play",
		"search_id":          "00000000-0000-4000-8000-000000000001",
		"event_id":           "00000000-0000-4000-8000-000000000002",
		"client_occurred_at": "2026-01-02T03:04:05Z",
		"payload":            map[string]any{"session_id": "s-1", "dwell_ms": 1500},
	}

	wireSend(t, router, http.MethodPost, "/discovery/events", "req_discovery_events", request, http.StatusNoContent)
	if len(store.events) != 1 {
		t.Fatalf("recorded events = %d, want 1", len(store.events))
	}
}

func TestWire_DiscoveryEventPayloadTypes(t *testing.T) {
	keys := []string{
		domain.PayloadKeySessionId, domain.PayloadKeyResultSignature,
		domain.PayloadKeyDwellMs, domain.PayloadKeyTailNoiseTop5, domain.PayloadKeyZeroResult,
	}
	values := map[string]any{"string": "x", "number": 1, "boolean": true, "null": nil}
	accepted := map[string][]string{}
	for _, key := range keys {
		accepted[key] = []string{}
		for kind, value := range values {
			router := buildEventRouter(t, &recordingEventStore{})
			body := map[string]any{"type": "play", "payload": map[string]any{key: value}}
			rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
			if rec.Code == http.StatusNoContent {
				accepted[key] = append(accepted[key], kind)
			}
		}
		sort.Strings(accepted[key])
	}

	jsoncontract.AssertWireExact(t, "discovery_event_payload_types", marshalWire(t, accepted))
}
