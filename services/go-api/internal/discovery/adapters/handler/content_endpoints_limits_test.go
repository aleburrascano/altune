package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
	"net/http"
	"reflect"
	"strings"
	"testing"

	discdomain "altune/go-api/internal/discovery/domain"

	"github.com/go-chi/chi/v5"
)

func contentTrack(title string) discdomain.SearchResult {
	return discdomain.SearchResult{
		Kind:       discdomain.ResultKindTrack,
		Title:      title,
		Confidence: discdomain.ConfidenceLow,
		Sources: []discdomain.SourceRef{
			{Provider: discdomain.ProviderDeezer, ExternalID: title, URL: "https://deezer.com/" + title},
		},
	}
}

func seedTracks(n int) []discdomain.SearchResult {
	out := make([]discdomain.SearchResult, n)
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
		{"absent limit uses default 50", "", 3},
		{"non-positive limit falls back to default", "?limit=-5", 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			albumProviders := map[discdomain.ProviderName]ports.AlbumContentProvider{
				discdomain.ProviderDeezer: &fakeAlbumContentProvider{results: seedTracks(3)},
			}
			router := buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{}, albumProviders, nil)

			rec := discServe(t, router, http.MethodGet, "/discovery/albums/deezer/1/tracks"+c.query, nil)
			discAssertStatus(t, rec, http.StatusOK)

			var resp ContentFetchResponseDTO
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
			artistProviders := map[discdomain.ProviderName]ports.ArtistContentProvider{
				discdomain.ProviderDeezer: &fakeArtistContentProvider{topTracks: seedTracks(7)},
			}
			router := buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{}, nil, artistProviders)

			rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/top-tracks"+c.query, nil)
			discAssertStatus(t, rec, http.StatusOK)

			var resp ContentFetchResponseDTO
			discDecodeJSON(t, rec, &resp)
			if len(resp.Items) != c.wantItems {
				t.Errorf("len(Items) = %d, want %d", len(resp.Items), c.wantItems)
			}
		})
	}
}

func TestContentEndpoints_ExternalIDTooLongReturns400(t *testing.T) {
	albumProviders := map[discdomain.ProviderName]ports.AlbumContentProvider{
		discdomain.ProviderDeezer: &fakeAlbumContentProvider{},
	}
	router := buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{}, albumProviders, nil)

	longID := strings.Repeat("a", 257)
	rec := discServe(t, router, http.MethodGet, "/discovery/albums/deezer/"+longID+"/tracks", nil)
	discAssertStatus(t, rec, http.StatusBadRequest)
}

func TestContentEndpoints_UnknownProviderOnArtistAndRelated(t *testing.T) {
	router := buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{}, nil, nil)
	rec := discServe(t, router, http.MethodGet, "/discovery/tracks/not_a_provider/1/related", nil)
	discAssertStatus(t, rec, http.StatusBadRequest)
}

func TestContentEndpoints_NilServiceDegradedEnvelope(t *testing.T) {
	router := buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{}, nil, nil)

	paths := []string{
		"/discovery/albums/deezer/1/tracks",
		"/discovery/artists/deezer/1/top-tracks",
		"/discovery/artists/deezer/1/albums",
		"/discovery/tracks/deezer/1/related",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			rec := discServe(t, router, http.MethodGet, path, nil)
			discAssertStatus(t, rec, http.StatusNotFound)
			discAssertJSON(t, rec)

			var resp ContentFetchResponseDTO
			discDecodeJSON(t, rec, &resp)
			if resp.Status != "error" || resp.Code != contentCodeUnserved {
				t.Errorf("status = %q, code = %q, want error / %s (service not wired)", resp.Status, resp.Code, contentCodeUnserved)
			}
			if resp.Items == nil {
				t.Error("items must be [] in the degraded envelope, got null")
			}
			if resp.Provider != "deezer" {
				t.Errorf("provider_name = %q, want deezer", resp.Provider)
			}
		})
	}
}

func TestHandleRelatedTracks_LimitClamping(t *testing.T) {
	provider := &fakeRelatedTracksProvider{results: seedTracks(5)}
	svc := service.NewGetRelatedTracksService(map[string]ports.RelatedTracksProvider{
		"soundcloud": provider,
	})
	h := NewDiscoveryHandler(DiscoveryServices{Related: svc})
	router := chi.NewRouter()
	router.Use(auth.Middleware(discVerifyAsTestUser))
	router.Mount("/discovery", h.Routes())

	rec := discServe(t, router, http.MethodGet, "/discovery/tracks/soundcloud/1/related?limit=2", nil)
	discAssertStatus(t, rec, http.StatusOK)

	var resp ContentFetchResponseDTO
	discDecodeJSON(t, rec, &resp)
	if len(resp.Items) != 2 {
		t.Errorf("len(Items) = %d, want 2 (limit applied)", len(resp.Items))
	}
}

func TestHandleArtistContent_RecordsContentFetchTrace(t *testing.T) {
	artistProviders := map[discdomain.ProviderName]ports.ArtistContentProvider{
		discdomain.ProviderDeezer: &fakeArtistContentProvider{
			topTracks: seedTracks(1),
			albums:    seedTracks(1),
		},
	}
	tr := &fakeSearchTrace{}
	h := NewDiscoveryHandler(DiscoveryServices{
		Artist: service.NewGetArtistContentService(artistProviders),
	}).WithRequestTrace(tr)
	router := chi.NewRouter()
	router.Use(auth.Middleware(discVerifyAsTestUser))
	router.Mount("/discovery", h.Routes())

	discAssertStatus(t, discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/top-tracks", nil), http.StatusOK)
	discAssertStatus(t, discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/albums", nil), http.StatusOK)

	if len(tr.contentFetches) != 2 || tr.contentFetches[0] != "top_tracks" || tr.contentFetches[1] != "albums" {
		t.Errorf("contentFetches = %v, want [top_tracks albums]", tr.contentFetches)
	}
}

// Pins every field each call site hands the trace, so a transposed
// Provider/Artist/Status at any of the four RecordContentFetch sites fails.
func TestHandleArtistContent_TraceEventFields(t *testing.T) {
	artistProviders := map[discdomain.ProviderName]ports.ArtistContentProvider{
		discdomain.ProviderDeezer: &fakeArtistContentProvider{
			topTracks: seedTracks(1),
			albums:    seedTracks(1),
		},
	}
	tr := &fakeSearchTrace{}
	h := NewDiscoveryHandler(DiscoveryServices{
		Artist: service.NewGetArtistContentService(artistProviders),
	}).WithRequestTrace(tr)
	router := chi.NewRouter()
	router.Use(auth.Middleware(discVerifyAsTestUser))
	router.Mount("/discovery", h.Routes())

	for _, path := range []string{
		"/discovery/artists/deezer/1/top-tracks?name=Che",
		"/discovery/artists/deezer/1/albums?name=Che",
		"/discovery/artists/deezer/1/content?name=Che",
	} {
		discAssertStatus(t, discServe(t, router, http.MethodGet, path, nil), http.StatusOK)
	}

	want := []ports.ContentFetchEvent{
		{Kind: "top_tracks", Provider: "deezer", Artist: "", Status: "ok"},
		{Kind: "albums", Provider: "deezer", Artist: "Che", Status: "ok"},
		{Kind: "top_tracks", Provider: "deezer", Artist: "Che", Status: "ok"},
		{Kind: "albums", Provider: "deezer", Artist: "Che", Status: "ok"},
	}
	if !reflect.DeepEqual(tr.contentEvents, want) {
		t.Errorf("content events = %+v, want %+v", tr.contentEvents, want)
	}
}
