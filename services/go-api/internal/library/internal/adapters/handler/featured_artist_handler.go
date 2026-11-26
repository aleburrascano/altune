package handler

import (
	featuredservice "altune/go-api/internal/library/internal/core/featured"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/requestuser"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type FeaturedArtistHandler struct {
	backfillFeatured *featuredservice.FillFeaturedArtistsService
	listFeaturing    *featuredservice.ListFeaturingService
}

func NewFeaturedArtistHandler(
	backfillFeatured *featuredservice.FillFeaturedArtistsService,
	listFeaturing *featuredservice.ListFeaturingService,
) *FeaturedArtistHandler {
	return &FeaturedArtistHandler{
		backfillFeatured: backfillFeatured,
		listFeaturing:    listFeaturing,
	}
}

func (h *FeaturedArtistHandler) Routes(r chi.Router) {
	r.Get("/featuring", h.handleListFeaturing)
	r.Post("/featured-backfill", h.handleBackfillFeatured)
}

func (h *FeaturedArtistHandler) handleBackfillFeatured(w http.ResponseWriter, r *http.Request) {
	userId, ok := requestuser.RequireUserID(w, r)
	if !ok {
		return
	}
	_, offset, err := pageBounds(r)
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}
	result, err := h.backfillFeatured.Execute(r.Context(), userId, offset)
	if err != nil {
		httpserver.HandleServiceError(w, r, withBackfillProgress(err, result))
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, result)
}

func withBackfillProgress(err error, result *featuredservice.BackfillFeaturedResult) error {
	if result == nil || result.Scanned == 0 {
		return err
	}
	return fmt.Errorf("featured backfill stopped after scanned %d, updated %d, failed %d, next offset %d: %w",
		result.Scanned, result.Updated, result.Failed, result.NextOffset, err)
}

func (h *FeaturedArtistHandler) handleListFeaturing(w http.ResponseWriter, r *http.Request) {
	userId, ok := requestuser.RequireUserID(w, r)
	if !ok {
		return
	}
	fa, err := featuredArtistFromQuery(r.URL.Query())
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}

	tracks, err := h.listFeaturing.Execute(r.Context(), userId, fa)
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, httpserver.NewList(tracksToDTO(tracks)))
}

func featuredArtistFromQuery(q url.Values) (domain.FeaturedArtist, error) {
	deezerID, err := parseDeezerID(q.Get("deezer_id"))
	if err != nil {
		return domain.FeaturedArtist{}, err
	}
	name := q.Get("name")
	mbid := q.Get("mbid")
	if name == "" && mbid == "" && deezerID == 0 {
		return domain.FeaturedArtist{}, domain.ErrFeaturedArtistKeyRequired
	}
	return domain.FeaturedArtistForQuery(name, mbid, deezerID), nil
}

func parseDeezerID(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	deezerID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || deezerID <= 0 {
		return 0, domain.ErrInvalidDeezerID
	}
	return deezerID, nil
}
