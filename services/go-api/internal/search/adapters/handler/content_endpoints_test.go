package handler_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
)

func contentTrack(title string) search.SearchResult {
	return search.SearchResult{
		Kind:       search.ResultKindTrack,
		Title:      title,
		Confidence: search.ConfidenceLow,
		Sources: []search.ProviderLink{
			{Provider: search.ProviderDeezer, ExternalID: title, URL: "https://deezer.com/" + title},
		},
	}
}

func seedTracks(n int) []search.SearchResult {
	out := make([]search.SearchResult, n)
	for i := range out {
		out[i] = contentTrack(string(rune('a' + i)))
	}
	return out
}

func TestHandleAlbumTracks_LimitClamping(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		wantItems int
	}{
		{"explicit limit truncates", "?limit=2", 2},
		{"absent limit serves the whole album", "", 3},
		{"non-positive limit falls back to default", "?limit=-5", 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			albumProviders := map[search.ProviderName]search.AlbumContentProvider{
				search.ProviderDeezer: &search.FakeAlbumContentProvider{Results: seedTracks(3)},
			}
			router := buildDiscoveryRouter(t, nil, &fakeSearchHistoryRepo{}, albumProviders, nil)

			rec := discServe(t, router, http.MethodGet, "/discovery/albums/deezer/1/tracks"+c.query, nil)
			discAssertStatus(t, rec, http.StatusOK)

			var resp search.ContentFetchResponseDTO
			discDecodeJSON(t, rec, &resp)
			if len(resp.Items) != c.wantItems {
				t.Errorf("len(Items) = %d, want %d", len(resp.Items), c.wantItems)
			}
		})
	}
}

func TestHandleArtistTopTracks_LimitClamping(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		wantItems int
	}{
		{"absent limit uses default 5", "", 5},
		{"explicit limit truncates", "?limit=3", 3},
		{"limit above cap 50 clamps but keeps all 7", "?limit=100", 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			artistProviders := map[search.ProviderName]search.ArtistContentProvider{
				search.ProviderDeezer: &search.FakeArtistContentProvider{TopTracks: seedTracks(7)},
			}
			router := buildDiscoveryRouter(t, nil, &fakeSearchHistoryRepo{}, nil, artistProviders)

			rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/top-tracks"+c.query, nil)
			discAssertStatus(t, rec, http.StatusOK)

			var resp search.ContentFetchResponseDTO
			discDecodeJSON(t, rec, &resp)
			if len(resp.Items) != c.wantItems {
				t.Errorf("len(Items) = %d, want %d", len(resp.Items), c.wantItems)
			}
		})
	}
}

func TestContentEndpoints_ExternalIDTooLongReturns400(t *testing.T) {
	albumProviders := map[search.ProviderName]search.AlbumContentProvider{
		search.ProviderDeezer: &search.FakeAlbumContentProvider{},
	}
	router := buildDiscoveryRouter(t, nil, &fakeSearchHistoryRepo{}, albumProviders, nil)

	longID := strings.Repeat("a", 257)
	rec := discServe(t, router, http.MethodGet, "/discovery/albums/deezer/"+longID+"/tracks", nil)
	discAssertStatus(t, rec, http.StatusBadRequest)
}

func TestContentEndpoints_UnknownProviderOnArtistAndRelated(t *testing.T) {
	router := buildDiscoveryRouter(t, nil, &fakeSearchHistoryRepo{}, nil, nil)
	rec := discServe(t, router, http.MethodGet, "/discovery/tracks/not_a_provider/1/related", nil)
	discAssertStatus(t, rec, http.StatusBadRequest)
}

func TestContentEndpoints_NilServiceDegradedEnvelope(t *testing.T) {
	router := buildDiscoveryRouter(t, nil, &fakeSearchHistoryRepo{}, nil, nil)

	paths := []string{
		"/discovery/albums/deezer/1/tracks",
		"/discovery/artists/deezer/1/top-tracks",
		"/discovery/artists/deezer/1/albums",
		"/discovery/tracks/deezer/1/related",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			rec := discServe(t, router, http.MethodGet, path, nil)
			assertErrorCode(t, rec, http.StatusNotFound, "discovery.content_unserved")
		})
	}
}

func TestHandleRelatedTracks_LimitClamping(t *testing.T) {
	provider := &fakeRelatedTracksProvider{results: seedTracks(5)}
	svc := searchtest.NewRelatedTracks(t, map[string]search.RelatedTracksProvider{
		"soundcloud": provider,
	})
	h := search.NewDiscoveryHandler(search.DiscoveryServices{Related: svc})
	router := chi.NewRouter()
	router.Use(discAsTestUser)
	router.Mount("/discovery", h.Routes())

	rec := discServe(t, router, http.MethodGet, "/discovery/tracks/soundcloud/1/related?limit=2", nil)
	discAssertStatus(t, rec, http.StatusOK)

	var resp search.ContentFetchResponseDTO
	discDecodeJSON(t, rec, &resp)
	if len(resp.Items) != 2 {
		t.Errorf("len(Items) = %d, want 2 (limit applied)", len(resp.Items))
	}
}

type stubIdentityStore struct {
	xref map[search.ProviderKey]string
}

func (s stubIdentityStore) LookupByProviderID(context.Context, search.ResultKind, search.ProviderKey, string) (string, map[search.ProviderKey]string, bool) {
	return "mbid-artist", s.xref, true
}

type partialContentProvider struct {
	provider search.ProviderName
	down     bool
}

func (p partialContentProvider) result(kind search.ResultKind, id string) ([]search.SearchResult, error) {
	if p.down {
		return nil, errors.New("upstream unavailable")
	}
	return []search.SearchResult{{
		Kind:     kind,
		Title:    "Real Song",
		Subtitle: "Artist",
		Sources:  []search.ProviderLink{{Provider: p.provider, ExternalID: id}},
		Extras:   map[string]any{},
	}}, nil
}

func (p partialContentProvider) GetArtistTopTracks(_ context.Context, _ search.ProviderName, id string) ([]search.SearchResult, error) {
	return p.result(search.ResultKindTrack, id)
}

func (p partialContentProvider) GetArtistAlbums(_ context.Context, _ search.ProviderName, id string) ([]search.SearchResult, error) {
	return p.result(search.ResultKindAlbum, id)
}

func identityContentRouter(t testing.TB, down map[search.ProviderName]bool) chi.Router {
	all := []search.ProviderName{
		search.ProviderDeezer, search.ProviderMusicBrainz, search.ProviderSoundCloud,
		search.ProviderITunes, search.ProviderTheAudioDB,
		search.ProviderDiscogs, search.ProviderYouTube,
		search.ProviderAppleMusic, search.ProviderSpotify,
	}
	providers := make(map[search.ProviderName]search.ArtistContentProvider, len(all))
	xref := make(map[search.ProviderKey]string, len(all))
	for _, pn := range all {
		providers[pn] = partialContentProvider{provider: pn, down: down[pn]}
		xref[pn.Key()] = "id-" + pn.String()
	}
	svc := searchtest.NewArtistContent(t, providers, searchtest.ArtistIdentities(stubIdentityStore{xref: xref}))
	h := search.NewDiscoveryHandler(search.DiscoveryServices{Artist: svc})
	router := chi.NewRouter()
	router.Use(discAsTestUser)
	router.Mount("/discovery", h.Routes())
	return router
}

func allButDeezerDown() map[search.ProviderName]bool {
	return map[search.ProviderName]bool{
		search.ProviderMusicBrainz: true, search.ProviderSoundCloud: true,
		search.ProviderITunes: true, search.ProviderTheAudioDB: true,
		search.ProviderDiscogs: true, search.ProviderYouTube: true,
		search.ProviderAppleMusic: true, search.ProviderSpotify: true,
	}
}

func decodeContentWire(t *testing.T, raw json.RawMessage) (status string, items int, partial *bool) {
	t.Helper()
	var body struct {
		Status  string            `json:"status"`
		Items   []json.RawMessage `json:"items"`
		Partial *bool             `json:"partial"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return body.Status, len(body.Items), body.Partial
}

func TestArtistContentEndpoints_IdentityFanOutReportsPartial(t *testing.T) {
	cases := []struct {
		name        string
		down        map[search.ProviderName]bool
		wantPartial bool
	}{
		{name: "10 of 11 providers failed", down: allButDeezerDown(), wantPartial: true},
		{name: "every provider answered", down: nil, wantPartial: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := identityContentRouter(t, tc.down)
			bodies := map[string]json.RawMessage{}
			for _, path := range []string{"top-tracks", "albums"} {
				rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/id-deezer/"+path+"?name=Artist", nil)
				discAssertStatus(t, rec, http.StatusOK)
				bodies[path] = rec.Body.Bytes()
			}
			rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/id-deezer/content?name=Artist", nil)
			discAssertStatus(t, rec, http.StatusOK)
			var combined map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &combined); err != nil {
				t.Fatalf("decode combined: %v", err)
			}
			bodies["content.top_tracks"] = combined["top_tracks"]
			bodies["content.albums"] = combined["albums"]

			for label, raw := range bodies {
				status, items, partial := decodeContentWire(t, raw)
				if status != "ok" || items == 0 {
					t.Errorf("%s: status = %q, items = %d, want ok with items", label, status, items)
				}
				if partial == nil || *partial != tc.wantPartial {
					t.Errorf("%s: partial = %v, want %v (body %s)", label, partial, tc.wantPartial, raw)
				}
			}
		})
	}
}

type panickingArtistContentProvider struct{}

func (panickingArtistContentProvider) GetArtistTopTracks(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
	panic("top tracks exploded")
}

func (panickingArtistContentProvider) GetArtistAlbums(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
	panic("albums exploded")
}

func TestHandleArtistContent_PanickingProviderReturns500(t *testing.T) {
	artistProviders := map[search.ProviderName]search.ArtistContentProvider{
		search.ProviderDeezer: panickingArtistContentProvider{},
	}
	router := buildDiscoveryRouter(t, nil, &fakeSearchHistoryRepo{}, nil, artistProviders)

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/content", nil)
	discAssertStatus(t, rec, http.StatusInternalServerError)
}

func TestHandleArtistContent_EachPanickingPartIsLoggedUnderItsOwnEvent(t *testing.T) {
	ring := captureLogs(t)
	artistProviders := map[search.ProviderName]search.ArtistContentProvider{
		search.ProviderDeezer: panickingArtistContentProvider{},
	}
	router := buildDiscoveryRouter(t, nil, &fakeSearchHistoryRepo{}, nil, artistProviders)

	discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/content", nil)

	for _, event := range []string{"artist_content.top_tracks_panic", "artist_content.albums_panic"} {
		if _, ok := findRecord(ring, event); !ok {
			t.Errorf("no %q record: each part keeps its own panic event", event)
		}
	}
}

type countingArtistContentProvider struct {
	search.FakeArtistContentProvider
	calls int
}

func (p *countingArtistContentProvider) GetArtistTopTracks(ctx context.Context, name search.ProviderName, id string) ([]search.SearchResult, error) {
	p.calls++
	return p.FakeArtistContentProvider.GetArtistTopTracks(ctx, name, id)
}

func (p *countingArtistContentProvider) GetArtistAlbums(ctx context.Context, name search.ProviderName, id string) ([]search.SearchResult, error) {
	p.calls++
	return p.FakeArtistContentProvider.GetArtistAlbums(ctx, name, id)
}

func unauthenticatedDiscoveryRouter(h *search.DiscoveryHandler) chi.Router {
	r := chi.NewRouter()
	r.Mount("/discovery", h.Routes())
	return r
}

func TestHandleArtistContent_NoIdentityIs401AndSkipsService(t *testing.T) {
	provider := &countingArtistContentProvider{}
	h := search.NewDiscoveryHandler(search.DiscoveryServices{
		Artist: searchtest.NewArtistContent(t, map[search.ProviderName]search.ArtistContentProvider{
			search.ProviderDeezer: provider,
		}),
	})

	rec := discServeNoAuth(t, unauthenticatedDiscoveryRouter(h), http.MethodGet, "/discovery/artists/deezer/1/content")

	discAssertStatus(t, rec, http.StatusUnauthorized)
	if provider.calls != 0 {
		t.Errorf("artist provider called %d times, want 0", provider.calls)
	}
}

func TestContentRoutes_NoIdentityIs401(t *testing.T) {
	paths := []string{
		"/discovery/albums/deezer/1/tracks",
		"/discovery/artists/deezer/1/top-tracks",
		"/discovery/artists/deezer/1/albums",
		"/discovery/tracks/deezer/1/related",
	}
	router := unauthenticatedDiscoveryRouter(search.NewDiscoveryHandler(search.DiscoveryServices{}))
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			discAssertStatus(t, discServeNoAuth(t, router, http.MethodGet, path), http.StatusUnauthorized)
		})
	}
}

func TestArtistContentEndpoints_AskTheProvidersTheSearchCardKnows(t *testing.T) {
	var mu sync.Mutex
	asked := map[string]bool{}
	recording := func(pn search.ProviderName) *search.FakeArtistContentProvider {
		record := func(_ context.Context, _ search.ProviderName, id string) ([]search.SearchResult, error) {
			mu.Lock()
			asked[pn.String()+":"+id] = true
			mu.Unlock()
			return seedTracks(1), nil
		}
		return &search.FakeArtistContentProvider{GetAlbumsFn: record, GetTopTracksFn: record}
	}
	svc := searchtest.NewArtistContent(t, map[search.ProviderName]search.ArtistContentProvider{
		search.ProviderDeezer:  recording(search.ProviderDeezer),
		search.ProviderSpotify: recording(search.ProviderSpotify),
	})
	router := chi.NewRouter()
	router.Use(discAsTestUser)
	router.Mount("/discovery", search.NewDiscoveryHandler(search.DiscoveryServices{Artist: svc}).Routes())

	for _, path := range []string{"content", "albums", "top-tracks"} {
		asked = map[string]bool{}
		rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/d1/"+path+"?name=Che&ids=spotify:sp1,notaprovider:x", nil)

		discAssertStatus(t, rec, http.StatusOK)
		if !asked["deezer:d1"] || !asked["spotify:sp1"] {
			t.Errorf("%s asked %v, want deezer:d1 and spotify:sp1", path, asked)
		}
	}
}

func TestArtistContentEndpoints_IgnoreTheArtistName(t *testing.T) {
	svc := searchtest.NewArtistContent(t, map[search.ProviderName]search.ArtistContentProvider{
		search.ProviderDeezer: &search.FakeArtistContentProvider{Albums: seedTracks(1), TopTracks: seedTracks(1)},
	})
	router := chi.NewRouter()
	router.Use(discAsTestUser)
	router.Mount("/discovery", search.NewDiscoveryHandler(search.DiscoveryServices{Artist: svc}).Routes())

	for _, path := range []string{"content", "albums", "top-tracks"} {
		rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/d1/"+path+"?name="+strings.Repeat("a", 5000), nil)

		discAssertStatus(t, rec, http.StatusOK)
	}
}

func TestAlbumTracks_ServesLongAlbumsWholeAndPassesTheCardIDs(t *testing.T) {
	var gotSpotifyID string
	deezer := &search.FakeAlbumContentProvider{Results: seedTracks(120)}
	spotify := &search.FakeAlbumContentProvider{GetAlbumTracksFn: func(_ context.Context, _ search.ProviderName, id string) ([]search.SearchResult, error) {
		gotSpotifyID = id
		return nil, nil
	}}
	svc := searchtest.NewAlbumTracks(t, map[search.ProviderName]search.AlbumContentProvider{
		search.ProviderDeezer:  deezer,
		search.ProviderSpotify: spotify,
	})
	router := chi.NewRouter()
	router.Use(discAsTestUser)
	router.Mount("/discovery", search.NewDiscoveryHandler(search.DiscoveryServices{Album: svc}).Routes())

	rec := discServe(t, router, http.MethodGet, "/discovery/albums/deezer/d1/tracks?ids=spotify:sp1", nil)

	discAssertStatus(t, rec, http.StatusOK)
	if _, items, _ := decodeContentWire(t, rec.Body.Bytes()); items != 120 {
		t.Errorf("items = %d, want all 120 tracks", items)
	}
	if gotSpotifyID != "sp1" {
		t.Errorf("Spotify asked for %q, want sp1 from ids", gotSpotifyID)
	}
}

func numberedAlbums(n int) []search.SearchResult {
	out := make([]search.SearchResult, n)
	for i := range out {
		id := strconv.Itoa(i)
		out[i] = search.NewProviderResult(search.ResultKindAlbum, "Album "+id, "Artist", "",
			search.ProviderLink{Provider: search.ProviderDeezer, ExternalID: id}, nil)
	}
	return out
}

func TestHandleArtistAlbums_LimitClamping(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		wantItems int
	}{
		{"absent limit uses default 50", "", 50},
		{"limit above the max clamps to 100", "?limit=500", 100},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := searchtest.NewArtistContent(t, map[search.ProviderName]search.ArtistContentProvider{
				search.ProviderDeezer: &search.FakeArtistContentProvider{Albums: numberedAlbums(120)},
			})
			router := chi.NewRouter()
			router.Use(discAsTestUser)
			router.Mount("/discovery", search.NewDiscoveryHandler(search.DiscoveryServices{Artist: svc}).Routes())

			rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/albums"+c.query, nil)

			discAssertStatus(t, rec, http.StatusOK)
			if _, items, _ := decodeContentWire(t, rec.Body.Bytes()); items != c.wantItems {
				t.Errorf("items = %d, want %d", items, c.wantItems)
			}
		})
	}
}
