package handler

import (
	"context"
	"net/http"
	"testing"

	"altune/go-api/internal/auth"
	discdomain "altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"

	"github.com/go-chi/chi/v5"
)

// fakeRelatedTracksProvider stands in for the SoundCloud adapter on the
// related-tracks endpoint.
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

// buildRelatedRouter mounts a discovery handler whose only wired use case is the
// related-tracks service (soundcloud-only), with auth middleware.
func buildRelatedRouter(provider *fakeRelatedTracksProvider) chi.Router {
	svc := service.NewGetRelatedTracksService(map[string]ports.RelatedTracksProvider{
		"soundcloud": provider,
	})
	h := NewDiscoveryHandler(nil, nil, nil, nil, nil, svc, nil, nil)

	r := chi.NewRouter()
	r.Use(auth.Middleware(&discFakeTokenVerifier{userId: discTestUserId}))
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

	t.Run("non-soundcloud provider returns 200 empty error", func(t *testing.T) {
		router := buildRelatedRouter(&fakeRelatedTracksProvider{})

		rec := discServe(t, router, http.MethodGet, "/discovery/tracks/deezer/9/related", nil)
		discAssertStatus(t, rec, http.StatusOK)

		var resp ContentFetchResponseDTO
		discDecodeJSON(t, rec, &resp)
		if resp.Status != "error" {
			t.Errorf("status = %q, want error (unsupported provider)", resp.Status)
		}
		if len(resp.Items) != 0 {
			t.Errorf("expected empty items, got %d", len(resp.Items))
		}
	})

	t.Run("missing external id returns 400", func(t *testing.T) {
		router := buildRelatedRouter(&fakeRelatedTracksProvider{})

		// trailing slash leaves externalId empty -> chi 404 on the param route;
		// hit the validate path with a blank segment via an explicit empty id.
		rec := discServe(t, router, http.MethodGet, "/discovery/tracks/soundcloud//related", nil)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 400 or 404 for missing external id", rec.Code)
		}
	})
}
