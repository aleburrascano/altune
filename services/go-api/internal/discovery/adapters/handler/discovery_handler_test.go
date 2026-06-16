package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"altune/go-api/internal/auth"
	discdomain "altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// --- test constants ---

var (
	discTestUserUUID = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	discTestUserId   = shared.NewUserId(discTestUserUUID)
)

// --- fake token verifier ---

type discFakeTokenVerifier struct {
	userId shared.UserId
}

func (v *discFakeTokenVerifier) Verify(_ context.Context, _ string) (shared.UserId, error) {
	return v.userId, nil
}

// --- fake search provider ---

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

// --- fake search history repo ---

type fakeSearchHistoryRepo struct {
	entries []*discdomain.SearchHistoryEntry
	err     error
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
	if r.err != nil {
		return nil, r.err
	}
	if limit > len(r.entries) {
		limit = len(r.entries)
	}
	return r.entries[:limit], nil
}

// --- fake click repo ---

type fakeSearchClickRepo struct {
	clicks   []*discdomain.SearchClick
	err      error
	inserted bool
}

func (r *fakeSearchClickRepo) InsertIfOutsideWindow(_ context.Context, click *discdomain.SearchClick, _ int) (bool, error) {
	if r.err != nil {
		return false, r.err
	}
	r.clicks = append(r.clicks, click)
	return r.inserted, nil
}

// --- fake album content provider ---

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

// --- fake artist content provider ---

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

// --- helpers ---

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

// --- router builder ---

func buildDiscoveryRouter(
	searchProvider *fakeSearchProvider,
	historyRepo *fakeSearchHistoryRepo,
	clickRepo *fakeSearchClickRepo,
	albumProviders map[string]ports.AlbumContentProvider,
	artistProviders map[string]ports.ArtistContentProvider,
) chi.Router {
	var providers []ports.SearchProvider
	if searchProvider != nil {
		providers = append(providers, searchProvider)
	}

	cb := service.NewCircuitBreaker()
	searchSvc := service.NewSearchMusicService(providers, nil, historyRepo, cb)
	clickSvc := service.NewRecordClickService(clickRepo)
	historySvc := service.NewListSearchHistoryService(historyRepo)

	var albumSvc *service.GetAlbumTracksService
	if albumProviders != nil {
		albumSvc = service.NewGetAlbumTracksService(albumProviders)
	}

	var artistSvc *service.GetArtistContentService
	if artistProviders != nil {
		artistSvc = service.NewGetArtistContentService(artistProviders)
	}

	h := NewDiscoveryHandler(searchSvc, clickSvc, historySvc, albumSvc, artistSvc, nil)

	r := chi.NewRouter()
	r.Use(auth.Middleware(&discFakeTokenVerifier{userId: discTestUserId}))
	r.Mount("/discovery", h.Routes())
	return r
}

// ==================== Search ====================

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
			name:       "invalid kinds returns 422",
			query:      "?q=test&kinds=invalid_kind",
			wantStatus: http.StatusUnprocessableEntity,
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
			// Arrange
			provider := &fakeSearchProvider{
				name:    discdomain.ProviderDeezer,
				results: tt.results,
			}
			historyRepo := &fakeSearchHistoryRepo{}
			clickRepo := &fakeSearchClickRepo{inserted: true}
			router := buildDiscoveryRouter(provider, historyRepo, clickRepo, nil, nil)

			// Act
			rec := discServe(t, router, http.MethodGet, "/discovery/search"+tt.query, nil)

			// Assert
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
	// Arrange
	router := buildDiscoveryRouter(
		&fakeSearchProvider{name: discdomain.ProviderDeezer},
		&fakeSearchHistoryRepo{},
		&fakeSearchClickRepo{inserted: true},
		nil, nil,
	)

	// Act
	rec := discServeNoAuth(t, router, http.MethodGet, "/discovery/search?q=test")

	// Assert
	discAssertStatus(t, rec, http.StatusUnauthorized)
}

func TestHandleSearch_ResponseShape(t *testing.T) {
	// Arrange
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
	router := buildDiscoveryRouter(provider, &fakeSearchHistoryRepo{}, &fakeSearchClickRepo{inserted: true}, nil, nil)

	// Act
	rec := discServe(t, router, http.MethodGet, "/discovery/search?q=shape+test", nil)

	// Assert
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

// ==================== Search History ====================

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
			// Arrange
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
			router := buildDiscoveryRouter(nil, historyRepo, &fakeSearchClickRepo{}, nil, nil)

			// Act
			rec := discServe(t, router, http.MethodGet, "/discovery/search-history?limit=10", nil)

			// Assert
			discAssertStatus(t, rec, tt.wantStatus)
			discAssertJSON(t, rec)

			var resp DiscoverySearchHistoryResponse
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

// ==================== Record Click ====================

func TestHandleRecordClick(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		wantStatus int
	}{
		{
			name: "valid click returns 204",
			body: DiscoveryClickRequest{
				QueryNorm:  "test",
				Kind:       "track",
				Title:      "Test Song",
				Subtitle:   "Test Artist",
				Position:   0,
				Confidence: "high",
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "invalid kind returns 400",
			body: DiscoveryClickRequest{
				QueryNorm:  "test",
				Kind:       "invalid_kind",
				Title:      "Test Song",
				Subtitle:   "Test Artist",
				Position:   0,
				Confidence: "high",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid confidence returns 400",
			body: DiscoveryClickRequest{
				QueryNorm:  "test",
				Kind:       "track",
				Title:      "Test Song",
				Subtitle:   "Test Artist",
				Position:   0,
				Confidence: "invalid_conf",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid JSON returns 400",
			body:       nil,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			clickRepo := &fakeSearchClickRepo{inserted: true}
			router := buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{}, clickRepo, nil, nil)

			// Act
			var rec *httptest.ResponseRecorder
			if tt.body == nil {
				rec = discServe(t, router, http.MethodPost, "/discovery/clicks", strings.NewReader("{invalid"))
			} else {
				rec = discServe(t, router, http.MethodPost, "/discovery/clicks", discJsonBody(t, tt.body))
			}

			// Assert
			discAssertStatus(t, rec, tt.wantStatus)
		})
	}
}

// ==================== Album Tracks ====================

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
			name:       "unknown provider returns OK with error status",
			path:       "/discovery/albums/unknown_provider/12345/tracks",
			albumProv:  &fakeAlbumContentProvider{results: []discdomain.SearchResult{}},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			albumProviders := map[string]ports.AlbumContentProvider{
				"deezer": tt.albumProv,
			}
			router := buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{}, &fakeSearchClickRepo{}, albumProviders, nil)

			// Act
			rec := discServe(t, router, http.MethodGet, tt.path, nil)

			// Assert
			discAssertStatus(t, rec, tt.wantStatus)
			discAssertJSON(t, rec)

			var resp ContentFetchResponseDTO
			discDecodeJSON(t, rec, &resp)
			if resp.Provider == "" {
				t.Error("expected non-empty provider_name in response")
			}
		})
	}
}

// ==================== Artist Top Tracks ====================

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
			name:       "unknown provider returns OK with error status",
			path:       "/discovery/artists/unknown/789/top-tracks",
			artistProv: &fakeArtistContentProvider{},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			artistProviders := map[string]ports.ArtistContentProvider{
				"deezer": tt.artistProv,
			}
			router := buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{}, &fakeSearchClickRepo{}, nil, artistProviders)

			// Act
			rec := discServe(t, router, http.MethodGet, tt.path, nil)

			// Assert
			discAssertStatus(t, rec, tt.wantStatus)
			discAssertJSON(t, rec)
		})
	}
}

// ==================== Artist Albums ====================

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
			name:       "unknown provider returns OK with error status",
			path:       "/discovery/artists/unknown/789/albums",
			artistProv: &fakeArtistContentProvider{},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			artistProviders := map[string]ports.ArtistContentProvider{
				"deezer": tt.artistProv,
			}
			router := buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{}, &fakeSearchClickRepo{}, nil, artistProviders)

			// Act
			rec := discServe(t, router, http.MethodGet, tt.path, nil)

			// Assert
			discAssertStatus(t, rec, tt.wantStatus)
			discAssertJSON(t, rec)
		})
	}
}
