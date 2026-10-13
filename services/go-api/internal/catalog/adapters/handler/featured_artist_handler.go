package handler

import (
	"altune/go-api/internal/catalog/adapters/authbridge"
	"altune/go-api/internal/catalog/domain"
	featuredservice "altune/go-api/internal/catalog/service/featured"
	"altune/go-api/internal/shared/httputil"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type FeaturedArtistHandler struct {
	backfillFeatured *featuredservice.BackfillFeaturedService
	listFeaturing    *featuredservice.ListFeaturingService
}

func NewFeaturedArtistHandler(
	backfillFeatured *featuredservice.BackfillFeaturedService,
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
	userId, ok := authbridge.RequireUserID(w, r)
	if !ok {
		return
	}
	_, offset := pageBounds(r)
	result, err := h.backfillFeatured.Execute(r.Context(), userId, offset)
	if err != nil {
		logPartialBackfill(r, result)
		httputil.HandleServiceError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, result)
}

func logPartialBackfill(r *http.Request, result *featuredservice.BackfillFeaturedResult) {
	if result == nil || result.Scanned == 0 {
		return
	}
	slog.WarnContext(r.Context(), "featured_backfill.partial",
		"scanned", result.Scanned,
		"updated", result.Updated,
		"failed", result.Failed,
		"next_offset", result.NextOffset,
	)
}

func (h *FeaturedArtistHandler) handleListFeaturing(w http.ResponseWriter, r *http.Request) {
	userId, ok := authbridge.RequireUserID(w, r)
	if !ok {
		return
	}
	fa, err := featuredArtistFromQuery(r.URL.Query())
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	tracks, err := h.listFeaturing.Execute(r.Context(), userId, fa)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, httputil.NewList(tracksToDTO(tracks)))
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
