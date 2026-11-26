package handler_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/requestuser"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var (
	discTestUserUUID = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	discTestUserId   = ids.NewUserId(discTestUserUUID)
)

func signedInAs(userFor func(token string) ids.UserId) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, signedIn := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !signedIn {
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(requestuser.ContextWithUserID(r.Context(), userFor(token))))
		})
	}
}

var discAsTestUser = signedInAs(func(string) ids.UserId { return discTestUserId })

type fakeSearchProvider struct {
	name    search.ProviderName
	results []search.SearchResult
	err     error
}

func (p *fakeSearchProvider) Name() search.ProviderName { return p.name }
func (p *fakeSearchProvider) Search(_ context.Context, _ string, _ map[search.ResultKind]bool) ([]search.SearchResult, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.results, nil
}

func (p *fakeSearchProvider) SupportedKinds() map[search.ResultKind]bool {
	return map[search.ResultKind]bool{
		search.ResultKindTrack:  true,
		search.ResultKindAlbum:  true,
		search.ResultKindArtist: true,
	}
}

type fakeSearchHistoryRepo struct {
	entries   []*search.SearchHistoryEntry
	err       error
	lastLimit int
}

func (r *fakeSearchHistoryRepo) Insert(_ context.Context, entry *search.SearchHistoryEntry) error {
	if r.err != nil {
		return r.err
	}
	r.entries = append(r.entries, entry)
	return nil
}

func (r *fakeSearchHistoryRepo) TrimToN(_ context.Context, _ ids.UserId, _ int) error {
	return nil
}

func (r *fakeSearchHistoryRepo) ListDistinctRecent(_ context.Context, _ ids.UserId, limit int) ([]*search.SearchHistoryEntry, error) {
	r.lastLimit = limit
	if r.err != nil {
		return nil, r.err
	}
	if limit > len(r.entries) {
		limit = len(r.entries)
	}
	return r.entries[:limit], nil
}

func (r *fakeSearchHistoryRepo) EraseSearchTextForUser(_ context.Context, _ ids.UserId) error {
	if r.err != nil {
		return r.err
	}
	r.entries = nil
	return nil
}

func discServe(t *testing.T, router chi.Router, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer fake-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func discServeNoAuth(t *testing.T, router chi.Router, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func discJsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(v); err != nil {
		t.Fatalf("discJsonBody: %v", err)
	}
	return buf
}

func discDecodeJSON(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(dst); err != nil {
		t.Fatalf("discDecodeJSON: %v (body: %s)", err, rec.Body.String())
	}
}

func discAssertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Errorf("status = %d, want %d (body: %s)", rec.Code, want, rec.Body.String())
	}
}

func discAssertJSON(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" && ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func discAssertProblem(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
}

func buildDiscoveryRouter(t *testing.T,
	searchProvider *fakeSearchProvider,
	historyRepo *fakeSearchHistoryRepo,
	albumProviders map[search.ProviderName]search.AlbumContentProvider,
	artistProviders map[search.ProviderName]search.ArtistContentProvider,
) chi.Router {
	bg := searchtest.NewTracker(t)
	var providers []search.SearchProvider
	if searchProvider != nil {
		providers = append(providers, searchProvider)
	}

	cb := search.NewCircuitBreaker()
	withHistory := func(*search.Collaborators) {}
	if historyRepo != nil {
		withHistory = func(c *search.Collaborators) { c.History = historyRepo }
	}
	searchSvc := searchtest.NewSearchServiceWith(t, bg, providers, cb, withHistory)
	historySvc := search.NewListSearchHistoryService(historyRepo)
	clearHistorySvc := search.NewClearSearchHistoryService(historyRepo)

	var albumSvc *search.GetAlbumTracksService
	if albumProviders != nil {
		albumSvc = search.NewGetAlbumTracksService(albumProviders)
	}

	var artistSvc *search.GetArtistContentService
	if artistProviders != nil {
		artistSvc = search.NewGetArtistContentService(artistProviders)
	}

	h := search.NewDiscoveryHandler(search.DiscoveryServices{
		Search:        searchSvc,
		History:       historySvc,
		RecordHistory: searchtest.NewHistoryRecorder(t, historyRepo),
		ClearHistory:  clearHistorySvc,
		Album:         albumSvc,
		Artist:        artistSvc,
	})

	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

func TestHandleRecordEvent(t *testing.T) {
	router := buildDiscoveryRouter(t, nil, nil, nil, nil)

	t.Run("valid event returns 204", func(t *testing.T) {
		body := map[string]any{"type": "play", "payload": map[string]any{"video_id": "abc"}}
		rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
		discAssertStatus(t, rec, http.StatusNoContent)
	})

	t.Run("unknown event type returns 400", func(t *testing.T) {
		body := map[string]any{"type": "bogus"}
		rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("malformed body returns 400", func(t *testing.T) {
		rec := discServe(t, router, http.MethodPost, "/discovery/events", strings.NewReader("{invalid"))
		discAssertStatus(t, rec, http.StatusBadRequest)
	})
}

type nopEventStore struct{}

func (nopEventStore) Append(context.Context, search.InteractionEvent) error { return nil }

func TestHandleRecordEvent_ServiceValidation(t *testing.T) {
	bg := searchtest.NewTracker(t)
	h := search.NewDiscoveryHandler(search.DiscoveryServices{
		Search: searchtest.NewSearchService(t, bg, nil, search.NewCircuitBreaker()),
		Event:  search.NewRecordEventService(nopEventStore{}),
	})
	router := chi.NewRouter()
	router.Use(discAsTestUser)
	router.Mount("/discovery", h.Routes())

	t.Run("server-reserved type returns 400", func(t *testing.T) {
		body := map[string]any{"type": "search_performed"}
		rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("mistyped payload value returns 400", func(t *testing.T) {
		body := map[string]any{"type": "skip", "payload": map[string]any{"dwell_ms": "abc"}}
		rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("well-formed interaction event returns 204", func(t *testing.T) {
		body := map[string]any{"type": "skip", "payload": map[string]any{"dwell_ms": 1500, "result_signature": "sig"}}
		rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
		discAssertStatus(t, rec, http.StatusNoContent)
	})
}

func TestHandleSearch(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		results    []search.SearchResult
		wantStatus int
	}{
		{
			name:  "valid query returns results",
			query: "?q=test+query",
			results: []search.SearchResult{
				{
					Kind:       search.ResultKindTrack,
					Title:      "Test Song",
					Subtitle:   "Test Artist",
					Confidence: search.ConfidenceLow,
					Sources: []search.ProviderLink{
						{Provider: search.ProviderDeezer, ExternalID: "123", URL: "https://deezer.com/123"},
					},
				},
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing q returns 400",
			query:      "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid kinds returns 400",
			query:      "?q=test&kinds=invalid_kind",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "valid query with explicit kinds",
			query:      "?q=test&kinds=track,album",
			results:    []search.SearchResult{},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeSearchProvider{
				name:    search.ProviderDeezer,
				results: tt.results,
			}
			historyRepo := &fakeSearchHistoryRepo{}
			router := buildDiscoveryRouter(t, provider, historyRepo, nil, nil)

			rec := discServe(t, router, http.MethodGet, "/discovery/search"+tt.query, nil)

			discAssertStatus(t, rec, tt.wantStatus)

			if tt.wantStatus == http.StatusOK {
				discAssertJSON(t, rec)
				var resp search.DiscoverySearchResponse
				discDecodeJSON(t, rec, &resp)
				if resp.Query == "" {
					t.Error("expected non-empty query in response")
				}
				if resp.Results == nil {
					t.Error("expected non-nil results array in response")
				}
				if resp.Providers == nil {
					t.Error("expected non-nil providers array in response")
				}
			}
		})
	}
}

func TestHandleSearch_NoAuth(t *testing.T) {
	router := buildDiscoveryRouter(t, &fakeSearchProvider{name: search.ProviderDeezer}, &fakeSearchHistoryRepo{}, nil, nil)

	rec := discServeNoAuth(t, router, http.MethodGet, "/discovery/search?q=test")

	discAssertStatus(t, rec, http.StatusUnauthorized)
}

func TestHandleSearch_ResponseShape(t *testing.T) {
	provider := &fakeSearchProvider{
		name: search.ProviderDeezer,
		results: []search.SearchResult{
			{
				Kind:       search.ResultKindTrack,
				Title:      "Shape Test",
				Subtitle:   "Shape Artist",
				ImageURL:   "https://img.example.com/art.jpg",
				Confidence: search.ConfidenceHigh,
				Sources: []search.ProviderLink{
					{Provider: search.ProviderDeezer, ExternalID: "456", URL: "https://deezer.com/456"},
				},
				Extras: map[string]any{"duration": 180},
			},
		},
	}
	router := buildDiscoveryRouter(t, provider, &fakeSearchHistoryRepo{}, nil, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q=shape+test", nil)

	discAssertStatus(t, rec, http.StatusOK)
	var raw map[string]json.RawMessage
	discDecodeJSON(t, rec, &raw)

	requiredFields := []string{"query", "query_norm", "results", "providers", "partial", "cache"}
	for _, f := range requiredFields {
		if _, ok := raw[f]; !ok {
			t.Errorf("response missing required field %q", f)
		}
	}
}

func TestHandleSearchHistory(t *testing.T) {
	tests := []struct {
		name         string
		seedEntries  int
		wantStatus   int
		wantItemsLen int
	}{
		{
			name:         "returns seeded history entries",
			seedEntries:  3,
			wantStatus:   http.StatusOK,
			wantItemsLen: 3,
		},
		{
			name:         "empty history returns empty items",
			seedEntries:  0,
			wantStatus:   http.StatusOK,
			wantItemsLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			historyRepo := &fakeSearchHistoryRepo{}
			for i := 0; i < tt.seedEntries; i++ {
				historyRepo.entries = append(historyRepo.entries, &search.SearchHistoryEntry{
					ID:         ids.NewEntityId(),
					UserId:     discTestUserId,
					Query:      "test query",
					QueryNorm:  "test query",
					ExecutedAt: time.Now().UTC(),
				})
			}
			router := buildDiscoveryRouter(t, nil, historyRepo, nil, nil)

			rec := discServe(t, router, http.MethodGet, "/discovery/search-history?limit=10", nil)

			discAssertStatus(t, rec, tt.wantStatus)
			discAssertJSON(t, rec)

			var resp httpserver.List[search.SearchHistoryItemDTO]
			discDecodeJSON(t, rec, &resp)
			if len(resp.Items) != tt.wantItemsLen {
				t.Errorf("len(Items) = %d, want %d", len(resp.Items), tt.wantItemsLen)
			}
			if resp.Total != tt.wantItemsLen {
				t.Errorf("Total = %d, want %d", resp.Total, tt.wantItemsLen)
			}
		})
	}
}

func TestHandleClearSearchHistory(t *testing.T) {
	historyRepo := &fakeSearchHistoryRepo{}
	for i := 0; i < 3; i++ {
		historyRepo.entries = append(historyRepo.entries, &search.SearchHistoryEntry{
			ID:         ids.NewEntityId(),
			UserId:     discTestUserId,
			Query:      "test query",
			QueryNorm:  "test query",
			ExecutedAt: time.Now().UTC(),
		})
	}
	router := buildDiscoveryRouter(t, nil, historyRepo, nil, nil)

	rec := discServe(t, router, http.MethodDelete, "/discovery/search-history", nil)

	discAssertStatus(t, rec, http.StatusNoContent)
	if len(historyRepo.entries) != 0 {
		t.Errorf("expected repo entries cleared, got %d", len(historyRepo.entries))
	}

	getRec := discServe(t, router, http.MethodGet, "/discovery/search-history?limit=10", nil)
	discAssertStatus(t, getRec, http.StatusOK)
	var resp httpserver.List[search.SearchHistoryItemDTO]
	discDecodeJSON(t, getRec, &resp)
	if len(resp.Items) != 0 {
		t.Errorf("expected 0 items after clear, got %d", len(resp.Items))
	}
}

func TestHandleAlbumTracks(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		albumProv  *search.FakeAlbumContentProvider
		wantStatus int
	}{
		{
			name: "valid request returns OK with items",
			path: "/discovery/albums/deezer/12345/tracks",
			albumProv: &search.FakeAlbumContentProvider{
				Results: []search.SearchResult{
					{
						Kind:       search.ResultKindTrack,
						Title:      "Album Track 1",
						Confidence: search.ConfidenceLow,
						Sources: []search.ProviderLink{
							{Provider: search.ProviderDeezer, ExternalID: "t1", URL: "https://deezer.com/t1"},
						},
					},
				},
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "unknown provider returns 400",
			path:       "/discovery/albums/unknown_provider/12345/tracks",
			albumProv:  &search.FakeAlbumContentProvider{Results: []search.SearchResult{}},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			albumProviders := map[search.ProviderName]search.AlbumContentProvider{
				search.ProviderDeezer: tt.albumProv,
			}
			router := buildDiscoveryRouter(t, nil, &fakeSearchHistoryRepo{}, albumProviders, nil)

			rec := discServe(t, router, http.MethodGet, tt.path, nil)

			discAssertStatus(t, rec, tt.wantStatus)
			if tt.wantStatus == http.StatusOK {
				discAssertJSON(t, rec)
			} else {
				discAssertProblem(t, rec)
			}

			if tt.wantStatus == http.StatusOK {
				var resp search.ContentFetchResponseDTO
				discDecodeJSON(t, rec, &resp)
				if resp.Provider == "" {
					t.Error("expected non-empty provider_name in response")
				}
			}
		})
	}
}

func TestHandleArtistTopTracks(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		artistProv *search.FakeArtistContentProvider
		wantStatus int
	}{
		{
			name: "valid request returns OK",
			path: "/discovery/artists/deezer/789/top-tracks",
			artistProv: &search.FakeArtistContentProvider{
				TopTracks: []search.SearchResult{
					{
						Kind:       search.ResultKindTrack,
						Title:      "Top Song",
						Confidence: search.ConfidenceLow,
						Sources: []search.ProviderLink{
							{Provider: search.ProviderDeezer, ExternalID: "t1", URL: "https://deezer.com/t1"},
						},
					},
				},
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "unknown provider returns 400",
			path:       "/discovery/artists/unknown/789/top-tracks",
			artistProv: &search.FakeArtistContentProvider{},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artistProviders := map[search.ProviderName]search.ArtistContentProvider{
				search.ProviderDeezer: tt.artistProv,
			}
			router := buildDiscoveryRouter(t, nil, &fakeSearchHistoryRepo{}, nil, artistProviders)

			rec := discServe(t, router, http.MethodGet, tt.path, nil)

			discAssertStatus(t, rec, tt.wantStatus)
			if tt.wantStatus == http.StatusOK {
				discAssertJSON(t, rec)
			} else {
				discAssertProblem(t, rec)
			}
		})
	}
}

func TestHandleArtistAlbums(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		artistProv *search.FakeArtistContentProvider
		wantStatus int
	}{
		{
			name: "valid request returns OK",
			path: "/discovery/artists/deezer/789/albums",
			artistProv: &search.FakeArtistContentProvider{
				Albums: []search.SearchResult{
					{
						Kind:       search.ResultKindAlbum,
						Title:      "Greatest Hits",
						Confidence: search.ConfidenceLow,
						Sources: []search.ProviderLink{
							{Provider: search.ProviderDeezer, ExternalID: "a1", URL: "https://deezer.com/a1"},
						},
					},
				},
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "unknown provider returns 400",
			path:       "/discovery/artists/unknown/789/albums",
			artistProv: &search.FakeArtistContentProvider{},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artistProviders := map[search.ProviderName]search.ArtistContentProvider{
				search.ProviderDeezer: tt.artistProv,
			}
			router := buildDiscoveryRouter(t, nil, &fakeSearchHistoryRepo{}, nil, artistProviders)

			rec := discServe(t, router, http.MethodGet, tt.path, nil)

			discAssertStatus(t, rec, tt.wantStatus)
			if tt.wantStatus == http.StatusOK {
				discAssertJSON(t, rec)
			} else {
				discAssertProblem(t, rec)
			}
		})
	}
}

type fakeMetadataEnricher struct {
	enrichment search.MusicBrainzEnrichment
}

func (f *fakeMetadataEnricher) ResolveMBID(_ context.Context, _ search.ResultKind, _, _ string) (string, error) {
	return "resolved-mbid", nil
}

func (f *fakeMetadataEnricher) Lookup(_ context.Context, _ search.ResultKind, _ string) (search.MusicBrainzEnrichment, error) {
	return f.enrichment, nil
}

func mustEnrichment(svc *search.DetailsService, err error) *search.DetailsService {
	if err != nil {
		panic(err)
	}
	return svc
}

func buildEnrichmentRouter(svc *search.DetailsService) chi.Router {
	h := search.NewDiscoveryHandler(search.DiscoveryServices{Enrich: svc})
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

func sampleAlbumEnrichment() search.MusicBrainzEnrichment {
	e := search.EmptyEnrichment()
	e.MBID = "resolved-mbid"
	e.Genres = []string{"conscious hip hop", "hip hop"}
	e.Year = 2017
	e.PrimaryType = "Album"
	e.ArtworkURL = "https://coverartarchive.org/x-1200.jpg"
	return e
}

func TestHandleEnrichment(t *testing.T) {
	t.Run("valid request returns enrichment DTO", func(t *testing.T) {
		svc := mustEnrichment(search.NewEnrichmentService(&fakeMetadataEnricher{enrichment: sampleAlbumEnrichment()}, search.NoArtwork{}, search.NoEnrichmentCache{}, search.NoMBIDIndex{}))
		router := buildEnrichmentRouter(svc)

		rec := discServe(t, router, http.MethodGet,
			"/discovery/enrichment?kind=album&title=DAMN.&subtitle=Kendrick+Lamar", nil)
		discAssertStatus(t, rec, http.StatusOK)
		discAssertJSON(t, rec)

		var resp search.EnrichmentResponseDTO
		discDecodeJSON(t, rec, &resp)
		if resp.Year != 2017 || resp.PrimaryType != "Album" {
			t.Errorf("unexpected DTO: %+v", resp)
		}
		if len(resp.Genres) != 2 || resp.Genres[0] != "conscious hip hop" {
			t.Errorf("genres = %v", resp.Genres)
		}
		if resp.ArtworkURL != "https://coverartarchive.org/x-1200.jpg" {
			t.Errorf("artwork_url = %q", resp.ArtworkURL)
		}
	})

	t.Run("missing kind returns 400", func(t *testing.T) {
		router := buildEnrichmentRouter(mustEnrichment(search.NewEnrichmentService(&fakeMetadataEnricher{}, search.NoArtwork{}, search.NoEnrichmentCache{}, search.NoMBIDIndex{})))
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment?title=DAMN.", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("unknown kind returns 400", func(t *testing.T) {
		router := buildEnrichmentRouter(mustEnrichment(search.NewEnrichmentService(&fakeMetadataEnricher{}, search.NoArtwork{}, search.NoEnrichmentCache{}, search.NoMBIDIndex{})))
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment?kind=playlistx&title=X", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("blank title and no mbid returns 400", func(t *testing.T) {
		router := buildEnrichmentRouter(mustEnrichment(search.NewEnrichmentService(&fakeMetadataEnricher{}, search.NoArtwork{}, search.NoEnrichmentCache{}, search.NoMBIDIndex{})))
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment?kind=album", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("nil service returns 503 enrichment_unavailable", func(t *testing.T) {
		router := buildEnrichmentRouter(nil)
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment?kind=album&title=X", nil)
		assertErrorCode(t, rec, http.StatusServiceUnavailable, "enrichment_unavailable")
	})
}

type fakeRelatedTracksProvider struct {
	results []search.SearchResult
	err     error
}

func (p *fakeRelatedTracksProvider) GetRelatedTracks(_ context.Context, _ search.ProviderName, _ string) ([]search.SearchResult, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.results, nil
}

func buildRelatedRouter(provider *fakeRelatedTracksProvider) chi.Router {
	svc := search.NewGetRelatedTracksService(map[string]search.RelatedTracksProvider{
		"soundcloud": provider,
	})
	h := search.NewDiscoveryHandler(search.DiscoveryServices{Related: svc})

	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

func TestHandleRelatedTracks(t *testing.T) {
	t.Run("soundcloud source returns mapped items", func(t *testing.T) {
		provider := &fakeRelatedTracksProvider{results: []search.SearchResult{
			{
				Kind:       search.ResultKindTrack,
				Title:      "Fell In Love",
				Subtitle:   "Lil Tecca",
				Confidence: search.ConfidenceLow,
				Sources: []search.ProviderLink{
					{Provider: search.ProviderSoundCloud, ExternalID: "555", URL: "https://soundcloud.com/x/fil"},
				},
			},
		}}
		router := buildRelatedRouter(provider)

		rec := discServe(t, router, http.MethodGet, "/discovery/tracks/soundcloud/12345/related", nil)
		discAssertStatus(t, rec, http.StatusOK)
		discAssertJSON(t, rec)

		var resp search.ContentFetchResponseDTO
		discDecodeJSON(t, rec, &resp)
		if resp.Status != "ok" {
			t.Errorf("status = %q, want ok", resp.Status)
		}
		if len(resp.Items) != 1 || resp.Items[0].Title != "Fell In Love" {
			t.Fatalf("unexpected items: %+v", resp.Items)
		}
	})

	t.Run("non-soundcloud provider returns 404 unserved", func(t *testing.T) {
		router := buildRelatedRouter(&fakeRelatedTracksProvider{})

		rec := discServe(t, router, http.MethodGet, "/discovery/tracks/deezer/9/related", nil)
		assertErrorCode(t, rec, http.StatusNotFound, "discovery.content_unserved")
	})

	t.Run("missing external id returns 400", func(t *testing.T) {
		router := buildRelatedRouter(&fakeRelatedTracksProvider{})

		rec := discServe(t, router, http.MethodGet, "/discovery/tracks/soundcloud//related", nil)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 400 or 404 for missing external id", rec.Code)
		}
	})
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	discAssertStatus(t, rec, wantStatus)
	discAssertProblem(t, rec)
	var resp httpserver.Problem
	discDecodeJSON(t, rec, &resp)
	if resp.Code != wantCode {
		t.Errorf("code = %q, want %q (detail: %q)", resp.Code, wantCode, resp.Detail)
	}
}

func rejectionCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body httpserver.Problem
	discDecodeJSON(t, rec, &body)
	if body.Code == "" {
		t.Fatalf("rejected request carried no code (detail %q)", body.Detail)
	}
	return body.Code
}

func TestPerUserGets_CarryNoStoreHeaders(t *testing.T) {
	searchRouter := buildDiscoveryRouter(t, &fakeSearchProvider{name: search.ProviderDeezer}, &fakeSearchHistoryRepo{}, nil, nil)
	favoritesRouter := buildFavoritesRouter(nil)
	tests := []struct {
		name string
		path string
		fav  bool
	}{
		{name: "search success", path: "/discovery/search?q=test"},
		{name: "search validation error", path: "/discovery/search"},
		{name: "history success", path: "/discovery/search-history"},
		{name: "favorites success", path: "/discovery/favorites", fav: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := searchRouter
			if tt.fav {
				router = favoritesRouter
			}
			rec := discServe(t, router, http.MethodGet, tt.path, nil)
			if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
				t.Errorf("Cache-Control = %q, want %q (status %d)", got, "private, no-store", rec.Code)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
		})
	}
}

func TestHandleRecordSearchHistory(t *testing.T) {
	t.Run("submitted query is saved once", func(t *testing.T) {
		historyRepo := &fakeSearchHistoryRepo{}
		router := buildDiscoveryRouter(t, nil, historyRepo, nil, nil)

		rec := discServe(t, router, http.MethodPost, "/discovery/search-history",
			discJsonBody(t, map[string]string{"query": "  Radiohead "}))

		discAssertStatus(t, rec, http.StatusNoContent)
		if len(historyRepo.entries) != 1 {
			t.Fatalf("history entries = %d, want 1", len(historyRepo.entries))
		}
		if got := historyRepo.entries[0]; got.Query != "Radiohead" || got.QueryNorm != "radiohead" {
			t.Errorf("saved query = %q / %q, want %q / %q", got.Query, got.QueryNorm, "Radiohead", "radiohead")
		}
	})

	rejected := []struct {
		name string
		body any
	}{
		{name: "blank query", body: map[string]string{"query": "   "}},
		{name: "missing query", body: map[string]string{}},
		{name: "query over the length cap", body: map[string]string{"query": strings.Repeat("a", 201)}},
		{name: "body is not an object", body: "radiohead"},
	}
	for _, tc := range rejected {
		t.Run(tc.name+" is rejected and nothing is saved", func(t *testing.T) {
			historyRepo := &fakeSearchHistoryRepo{}
			router := buildDiscoveryRouter(t, nil, historyRepo, nil, nil)

			rec := discServe(t, router, http.MethodPost, "/discovery/search-history", discJsonBody(t, tc.body))

			discAssertStatus(t, rec, http.StatusBadRequest)
			if len(historyRepo.entries) != 0 {
				t.Errorf("history entries = %d, want 0", len(historyRepo.entries))
			}
		})
	}

	t.Run("anonymous caller is refused", func(t *testing.T) {
		historyRepo := &fakeSearchHistoryRepo{}
		router := buildDiscoveryRouter(t, nil, historyRepo, nil, nil)

		rec := discServeNoAuth(t, router, http.MethodPost, "/discovery/search-history")

		discAssertStatus(t, rec, http.StatusUnauthorized)
		if len(historyRepo.entries) != 0 {
			t.Errorf("history entries = %d, want 0", len(historyRepo.entries))
		}
	})
}
