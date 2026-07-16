package handler

import (
	"context"
	"net/http"
	"testing"

	"altune/go-api/internal/auth"
	discdomain "altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service"

	"github.com/go-chi/chi/v5"
)

// fakeMetadataEnricher stands in for the MusicBrainz adapter.
type fakeMetadataEnricher struct {
	enrichment discdomain.MBEnrichment
}

func (f *fakeMetadataEnricher) ResolveMBID(_ context.Context, _ discdomain.ResultKind, _, _ string) (string, error) {
	return "resolved-mbid", nil
}

func (f *fakeMetadataEnricher) Lookup(_ context.Context, _ discdomain.ResultKind, _ string) (discdomain.MBEnrichment, error) {
	return f.enrichment, nil
}

func buildEnrichmentRouter(svc *service.EnrichmentService) chi.Router {
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
		svc := service.NewEnrichmentService(&fakeMetadataEnricher{enrichment: sampleAlbumEnrichment()}, nil, nil)
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
		router := buildEnrichmentRouter(service.NewEnrichmentService(&fakeMetadataEnricher{}, nil, nil))
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment?title=DAMN.", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("unknown kind returns 400", func(t *testing.T) {
		router := buildEnrichmentRouter(service.NewEnrichmentService(&fakeMetadataEnricher{}, nil, nil))
		rec := discServe(t, router, http.MethodGet, "/discovery/enrichment?kind=playlistx&title=X", nil)
		discAssertStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("blank title and no mbid returns 400", func(t *testing.T) {
		router := buildEnrichmentRouter(service.NewEnrichmentService(&fakeMetadataEnricher{}, nil, nil))
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
