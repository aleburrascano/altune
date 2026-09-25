package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/discovery/service/enrich"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	discdomain "altune/go-api/internal/discovery/domain"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var (
	discTestUserUUID = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	discTestUserId   = shared.NewUserId(discTestUserUUID)
)

var discVerifyAsTestUser = auth.VerifierFunc(func(context.Context, string) (auth.VerifiedToken, error) {
	return auth.VerifiedToken{UserID: discTestUserId, ExpiresAt: time.Now().Add(time.Hour)}, nil
})

type fakeSearchProvider struct {
	name    discdomain.ProviderName
	results []discdomain.SearchResult
	err     error
}

func (p *fakeSearchProvider) Name() discdomain.ProviderName { return p.name }
func (p *fakeSearchProvider) Search(_ context.Context, _ string, _ map[discdomain.ResultKind]bool) ([]discdomain.SearchResult, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.results, nil
}

func (p *fakeSearchProvider) SupportedKinds() map[discdomain.ResultKind]bool {
	return map[discdomain.ResultKind]bool{
		discdomain.ResultKindTrack:  true,
		discdomain.ResultKindAlbum:  true,
		discdomain.ResultKindArtist: true,
	}
}

type fakeSearchHistoryRepo struct {
	entries   []*discdomain.SearchHistoryEntry
	err       error
	lastLimit int
}

func (r *fakeSearchHistoryRepo) Insert(_ context.Context, entry *discdomain.SearchHistoryEntry) error {
	if r.err != nil {
		return r.err
	}
	r.entries = append(r.entries, entry)
	return nil
}

func (r *fakeSearchHistoryRepo) TrimToN(_ context.Context, _ shared.UserId, _ int) error {
	return nil
}

func (r *fakeSearchHistoryRepo) ListDistinctRecent(_ context.Context, _ shared.UserId, limit int) ([]*discdomain.SearchHistoryEntry, error) {
	r.lastLimit = limit
	if r.err != nil {
		return nil, r.err
	}
	if limit > len(r.entries) {
		limit = len(r.entries)
	}
	return r.entries[:limit], nil
}

func (r *fakeSearchHistoryRepo) EraseSearchTextForUser(_ context.Context, _ shared.UserId) error {
	if r.err != nil {
		return r.err
	}
	r.entries = nil
	return nil
}

type fakeAlbumContentProvider struct {
	results []discdomain.SearchResult
	err     error
}

func (p *fakeAlbumContentProvider) GetAlbumTracks(_ context.Context, _ discdomain.ProviderName, _ string) ([]discdomain.SearchResult, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.results, nil
}

type fakeArtistContentProvider struct {
	topTracks []discdomain.SearchResult
	albums    []discdomain.SearchResult
	err       error
}

func (p *fakeArtistContentProvider) GetArtistTopTracks(_ context.Context, _ discdomain.ProviderName, _ string) ([]discdomain.SearchResult, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.topTracks, nil
}

func (p *fakeArtistContentProvider) GetArtistAlbums(_ context.Context, _ discdomain.ProviderName, _ string) ([]discdomain.SearchResult, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.albums, nil
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

func buildDiscoveryRouter(
	searchProvider *fakeSearchProvider,
	historyRepo *fakeSearchHistoryRepo,
	albumProviders map[discdomain.ProviderName]ports.AlbumContentProvider,
	artistProviders map[discdomain.ProviderName]ports.ArtistContentProvider,
) chi.Router {
	var providers []ports.SearchProvider
	if searchProvider != nil {
		providers = append(providers, searchProvider)
	}

	cb := service.NewCircuitBreaker()
	var searchOpts []service.Option
	if historyRepo != nil {
		searchOpts = append(searchOpts, service.WithHistoryRepository(historyRepo))
	}
	searchSvc := service.NewService(providers, cb, searchOpts...)
	historySvc := service.NewListSearchHistoryService(historyRepo)
	clearHistorySvc := service.NewClearSearchHistoryService(historyRepo)

	var albumSvc *service.GetAlbumTracksService
	if albumProviders != nil {
		albumSvc = service.NewGetAlbumTracksService(albumProviders)
	}

	var artistSvc *service.GetArtistContentService
	if artistProviders != nil {
		artistSvc = service.NewGetArtistContentService(artistProviders)
	}

	h := NewDiscoveryHandler(DiscoveryServices{
		Search:       searchSvc,
		History:      historySvc,
		ClearHistory: clearHistorySvc,
		Album:        albumSvc,
		Artist:       artistSvc,
	})

	r := chi.NewRouter()
	r.Use(auth.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
}

func TestHandleRecordEvent(t *testing.T) {
	router := buildDiscoveryRouter(nil, nil, nil, nil)

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

func (nopEventStore) Append(context.Context, discdomain.InteractionEvent) error { return nil }

func TestHandleRecordEvent_ServiceValidation(t *testing.T) {
	h := NewDiscoveryHandler(DiscoveryServices{
		Search: service.NewService(nil, service.NewCircuitBreaker()),
		Event:  service.NewRecordEventService(nopEventStore{}),
	})
	router := chi.NewRouter()
	router.Use(auth.Middleware(discVerifyAsTestUser))
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
		results    []discdomain.SearchResult
		wantStatus int
	}{
		{
			name:  "valid query returns results",
			query: "?q=test+query",
			results: []discdomain.SearchResult{
				{
					Kind:       discdomain.ResultKindTrack,
					Title:      "Test Song",
					Subtitle:   "Test Artist",
					Confidence: discdomain.ConfidenceLow,
					Sources: []discdomain.SourceRef{
						{Provider: discdomain.ProviderDeezer, ExternalID: "123", URL: "https://deezer.com/123"},
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
			results:    []discdomain.SearchResult{},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeSearchProvider{
				name:    discdomain.ProviderDeezer,
				results: tt.results,
			}
			historyRepo := &fakeSearchHistoryRepo{}
			router := buildDiscoveryRouter(provider, historyRepo, nil, nil)

			rec := discServe(t, router, http.MethodGet, "/discovery/search"+tt.query, nil)

			discAssertStatus(t, rec, tt.wantStatus)

			if tt.wantStatus == http.StatusOK {
				discAssertJSON(t, rec)
				var resp DiscoverySearchResponse
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
	router := buildDiscoveryRouter(&fakeSearchProvider{name: discdomain.ProviderDeezer}, &fakeSearchHistoryRepo{}, nil, nil)

	rec := discServeNoAuth(t, router, http.MethodGet, "/discovery/search?q=test")

	discAssertStatus(t, rec, http.StatusUnauthorized)
}

func TestHandleSearch_ResponseShape(t *testing.T) {
	provider := &fakeSearchProvider{
		name: discdomain.ProviderDeezer,
		results: []discdomain.SearchResult{
			{
				Kind:       discdomain.ResultKindTrack,
				Title:      "Shape Test",
				Subtitle:   "Shape Artist",
				ImageURL:   "https://img.example.com/art.jpg",
				Confidence: discdomain.ConfidenceHigh,
				Sources: []discdomain.SourceRef{
					{Provider: discdomain.ProviderDeezer, ExternalID: "456", URL: "https://deezer.com/456"},
				},
				Extras: map[string]any{"duration": 180},
			},
		},
	}
	router := buildDiscoveryRouter(provider, &fakeSearchHistoryRepo{}, nil, nil)

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
				historyRepo.entries = append(historyRepo.entries, &discdomain.SearchHistoryEntry{
					ID:         uuid.New(),
					UserId:     discTestUserId,
					Query:      "test query",
					QueryNorm:  "test query",
					ExecutedAt: time.Now().UTC(),
				})
			}
			router := buildDiscoveryRouter(nil, historyRepo, nil, nil)

			rec := discServe(t, router, http.MethodGet, "/discovery/search-history?limit=10", nil)

			discAssertStatus(t, rec, tt.wantStatus)
			discAssertJSON(t, rec)

			var resp httputil.List[SearchHistoryItemDTO]
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
		historyRepo.entries = append(historyRepo.entries, &discdomain.SearchHistoryEntry{
			ID:         uuid.New(),
			UserId:     discTestUserId,
			Query:      "test query",
			QueryNorm:  "test query",
			ExecutedAt: time.Now().UTC(),
		})
	}
	router := buildDiscoveryRouter(nil, historyRepo, nil, nil)

	rec := discServe(t, router, http.MethodDelete, "/discovery/search-history", nil)

	discAssertStatus(t, rec, http.StatusNoContent)
	if len(historyRepo.entries) != 0 {
		t.Errorf("expected repo entries cleared, got %d", len(historyRepo.entries))
	}

	getRec := discServe(t, router, http.MethodGet, "/discovery/search-history?limit=10", nil)
	discAssertStatus(t, getRec, http.StatusOK)
	var resp httputil.List[SearchHistoryItemDTO]
	discDecodeJSON(t, getRec, &resp)
	if len(resp.Items) != 0 {
		t.Errorf("expected 0 items after clear, got %d", len(resp.Items))
	}
}

func TestHandleAlbumTracks(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		albumProv  *fakeAlbumContentProvider
		wantStatus int
	}{
		{
			name: "valid request returns OK with items",
			path: "/discovery/albums/deezer/12345/tracks",
			albumProv: &fakeAlbumContentProvider{
				results: []discdomain.SearchResult{
					{
						Kind:       discdomain.ResultKindTrack,
						Title:      "Album Track 1",
						Confidence: discdomain.ConfidenceLow,
						Sources: []discdomain.SourceRef{
							{Provider: discdomain.ProviderDeezer, ExternalID: "t1", URL: "https://deezer.com/t1"},
						},
					},
				},
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "unknown provider returns 400",
			path:       "/discovery/albums/unknown_provider/12345/tracks",
			albumProv:  &fakeAlbumContentProvider{results: []discdomain.SearchResult{}},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			albumProviders := map[discdomain.ProviderName]ports.AlbumContentProvider{
				discdomain.ProviderDeezer: tt.albumProv,
			}
			router := buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{}, albumProviders, nil)

			rec := discServe(t, router, http.MethodGet, tt.path, nil)

			discAssertStatus(t, rec, tt.wantStatus)
			discAssertJSON(t, rec)

			if tt.wantStatus == http.StatusOK {
				var resp ContentFetchResponseDTO
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
		artistProv *fakeArtistContentProvider
		wantStatus int
	}{
		{
			name: "valid request returns OK",
			path: "/discovery/artists/deezer/789/top-tracks",
			artistProv: &fakeArtistContentProvider{
				topTracks: []discdomain.SearchResult{
					{
						Kind:       discdomain.ResultKindTrack,
						Title:      "Top Song",
						Confidence: discdomain.ConfidenceLow,
						Sources: []discdomain.SourceRef{
							{Provider: discdomain.ProviderDeezer, ExternalID: "t1", URL: "https://deezer.com/t1"},
						},
					},
				},
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "unknown provider returns 400",
			path:       "/discovery/artists/unknown/789/top-tracks",
			artistProv: &fakeArtistContentProvider{},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artistProviders := map[discdomain.ProviderName]ports.ArtistContentProvider{
				discdomain.ProviderDeezer: tt.artistProv,
			}
			router := buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{}, nil, artistProviders)

			rec := discServe(t, router, http.MethodGet, tt.path, nil)

			discAssertStatus(t, rec, tt.wantStatus)
			discAssertJSON(t, rec)
		})
	}
}

func TestHandleArtistAlbums(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		artistProv *fakeArtistContentProvider
		wantStatus int
	}{
		{
			name: "valid request returns OK",
			path: "/discovery/artists/deezer/789/albums",
			artistProv: &fakeArtistContentProvider{
				albums: []discdomain.SearchResult{
					{
						Kind:       discdomain.ResultKindAlbum,
						Title:      "Greatest Hits",
						Confidence: discdomain.ConfidenceLow,
						Sources: []discdomain.SourceRef{
							{Provider: discdomain.ProviderDeezer, ExternalID: "a1", URL: "https://deezer.com/a1"},
						},
					},
				},
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "unknown provider returns 400",
			path:       "/discovery/artists/unknown/789/albums",
			artistProv: &fakeArtistContentProvider{},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artistProviders := map[discdomain.ProviderName]ports.ArtistContentProvider{
				discdomain.ProviderDeezer: tt.artistProv,
			}
			router := buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{}, nil, artistProviders)

			rec := discServe(t, router, http.MethodGet, tt.path, nil)

			discAssertStatus(t, rec, tt.wantStatus)
			discAssertJSON(t, rec)
		})
	}
}

type fakeMetadataEnricher struct {
	enrichment discdomain.MBEnrichment
}

func (f *fakeMetadataEnricher) ResolveMBID(_ context.Context, _ discdomain.ResultKind, _, _ string) (string, error) {
	return "resolved-mbid", nil
}

func (f *fakeMetadataEnricher) Lookup(_ context.Context, _ discdomain.ResultKind, _ string) (discdomain.MBEnrichment, error) {
	return f.enrichment, nil
}

func buildEnrichmentRouter(svc *enrich.EnrichmentService) chi.Router {
	h := NewDiscoveryHandler(DiscoveryServices{Enrich: svc})
	r := chi.NewRouter()
	r.Use(auth.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
}

func sampleAlbumEnrichment() discdomain.MBEnrichment {
	e := discdomain.EmptyEnrichment()
	e.MBID = "resolved-mbid"
	e.Genres = []string{"conscious hip hop", "hip hop"}
	e.Year = 2017
	e.PrimaryType = "Album"
	e.ArtworkURL = "https://coverartarchive.org/x-1200.jpg"
	return e
}

func TestHandleEnrichment(t *testing.T) {
	t.Run("valid request returns enrichment DTO", func(t *testing.T) {
		svc := enrich.NewEnrichmentService(&fakeMetadataEnricher{enrichment: sampleAlbumEnrichment()}, nil, nil)
		router := buildEnrichmentRouter(svc)

		rec := discServe(t, router, http.MethodGet,
			"/discovery/enrichment?kind=album&title=DAMN.&subtitle=Kendrick+Lamar", nil)
		discAssertStatus(t, rec, http.StatusOK)
		discAssertJSON(t, rec)

		var resp EnrichmentResponseDTO
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
		router := buildEnrichmentRouter(enrich.NewEnrichmentService(&fakeMetadataEnricher{}, nil, nil))
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment?title=DAMN.", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("unknown kind returns 400", func(t *testing.T) {
		router := buildEnrichmentRouter(enrich.NewEnrichmentService(&fakeMetadataEnricher{}, nil, nil))
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment?kind=playlistx&title=X", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("blank title and no mbid returns 400", func(t *testing.T) {
		router := buildEnrichmentRouter(enrich.NewEnrichmentService(&fakeMetadataEnricher{}, nil, nil))
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment?kind=album", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("nil service returns 200 empty DTO", func(t *testing.T) {
		router := buildEnrichmentRouter(nil)
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment?kind=album&title=X", nil)
		discAssertStatus(t, rec, http.StatusOK)

		var resp EnrichmentResponseDTO
		discDecodeJSON(t, rec, &resp)
		if resp.MBID != "" || len(resp.Genres) != 0 {
			t.Errorf("want empty DTO, got %+v", resp)
		}
		if resp.Genres == nil || resp.ExternalIDs == nil || resp.SecondaryTypes == nil {
			t.Error("DTO collections must be non-null even when empty")
		}
	})
}

type fakeRelatedTracksProvider struct {
	results []discdomain.SearchResult
	err     error
}

func (p *fakeRelatedTracksProvider) GetRelatedTracks(_ context.Context, _ discdomain.ProviderName, _ string) ([]discdomain.SearchResult, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.results, nil
}

func buildRelatedRouter(provider *fakeRelatedTracksProvider) chi.Router {
	svc := service.NewGetRelatedTracksService(map[string]ports.RelatedTracksProvider{
		"soundcloud": provider,
	})
	h := NewDiscoveryHandler(DiscoveryServices{Related: svc})

	r := chi.NewRouter()
	r.Use(auth.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
}

func TestHandleRelatedTracks(t *testing.T) {
	t.Run("soundcloud source returns mapped items", func(t *testing.T) {
		provider := &fakeRelatedTracksProvider{results: []discdomain.SearchResult{
			{
				Kind:       discdomain.ResultKindTrack,
				Title:      "Fell In Love",
				Subtitle:   "Lil Tecca",
				Confidence: discdomain.ConfidenceLow,
				Sources: []discdomain.SourceRef{
					{Provider: discdomain.ProviderSoundCloud, ExternalID: "555", URL: "https://soundcloud.com/x/fil"},
				},
			},
		}}
		router := buildRelatedRouter(provider)

		rec := discServe(t, router, http.MethodGet, "/discovery/tracks/soundcloud/12345/related", nil)
		discAssertStatus(t, rec, http.StatusOK)
		discAssertJSON(t, rec)

		var resp ContentFetchResponseDTO
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
		discAssertStatus(t, rec, http.StatusNotFound)

		var resp ContentFetchResponseDTO
		discDecodeJSON(t, rec, &resp)
		if resp.Status != "error" || resp.Code != contentCodeUnserved {
			t.Errorf("status = %q, code = %q, want error / %s (unsupported provider)", resp.Status, resp.Code, contentCodeUnserved)
		}
		if len(resp.Items) != 0 {
			t.Errorf("expected empty items, got %d", len(resp.Items))
		}
	})

	t.Run("missing external id returns 400", func(t *testing.T) {
		router := buildRelatedRouter(&fakeRelatedTracksProvider{})

		rec := discServe(t, router, http.MethodGet, "/discovery/tracks/soundcloud//related", nil)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 400 or 404 for missing external id", rec.Code)
		}
	})
}

// statusCodedError carries its own HTTP status and machine-readable code
// through the httputil StatusError/ErrorCoder contract.
type statusCodedError struct {
	status int
	code   string
}

func (e statusCodedError) Error() string { return "classified failure" }

func (e statusCodedError) HTTPStatus() int { return e.status }

func (e statusCodedError) ErrorCode() string { return e.code }

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	discAssertStatus(t, rec, wantStatus)
	discAssertJSON(t, rec)
	var resp httputil.ErrorResponse
	discDecodeJSON(t, rec, &resp)
	if resp.Code != wantCode {
		t.Errorf("code = %q, want %q (detail: %q)", resp.Code, wantCode, resp.Detail)
	}
}

// rejectionCode is the code a caller branches on, taken off a rejected
// request. An empty one is the failure this file exists to prevent.
func rejectionCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body httputil.ErrorResponse
	discDecodeJSON(t, rec, &body)
	if body.Code == "" {
		t.Fatalf("rejected request carried no code (detail %q)", body.Detail)
	}
	return body.Code
}
