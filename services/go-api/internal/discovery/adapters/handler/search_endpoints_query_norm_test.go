package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	discdomain "altune/go-api/internal/discovery/domain"

	"github.com/go-chi/chi/v5"
)

func buildQueryNormRouter(history *fakeSearchHistoryRepo) chi.Router {
	p := &fakeSearchProvider{
		name: discdomain.ProviderDeezer,
		results: []discdomain.SearchResult{{
			Kind: discdomain.ResultKindTrack, Title: "Song", Subtitle: "Artist",
			Confidence: discdomain.ConfidenceLow,
			Sources: []discdomain.SourceRef{
				{Provider: discdomain.ProviderDeezer, ExternalID: "1", URL: "https://deezer.com/1"},
			},
		}},
	}
	svc := service.NewService([]ports.SearchProvider{p}, service.NewCircuitBreaker(),
		service.WithHistoryRepository(history))
	h := NewDiscoveryHandler(DiscoveryServices{Search: svc})
	r := chi.NewRouter()
	r.Use(auth.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
}

// TestHandleSearch_QueryNorm_MatchesServiceCanonicalValue guards #1085: the
// response's query_norm must be the value the service computed (from the
// cleaned query) and persisted to history, not a re-normalization of the raw
// query string.
func TestHandleSearch_QueryNorm_MatchesServiceCanonicalValue(t *testing.T) {
	cases := []struct{ name, raw string }{
		{"noise stripped", "Humble Official Video"},
		{"trailing feat stripped", "Humble feat."},
		{"lyrics and hd stripped", "Kendrick Lamar - HUMBLE (Lyrics) HD"},
		{"no noise", "  Humble  "},
		{"all noise falls back to raw", "Official Video"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			history := &fakeSearchHistoryRepo{}
			router := buildQueryNormRouter(history)

			rec := discServe(t, router, http.MethodGet, "/discovery/search?q="+url.QueryEscape(tc.raw), nil)

			discAssertStatus(t, rec, http.StatusOK)
			var resp DiscoverySearchResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if len(history.entries) != 1 {
				t.Fatalf("history entries = %d, want 1", len(history.entries))
			}
			if got, want := resp.QueryNorm, history.entries[0].QueryNorm; got != want {
				t.Errorf("response query_norm = %q, service canonical queryNorm = %q", got, want)
			}
			if resp.Query != tc.raw {
				t.Errorf("response query = %q, want raw %q", resp.Query, tc.raw)
			}
		})
	}
}
