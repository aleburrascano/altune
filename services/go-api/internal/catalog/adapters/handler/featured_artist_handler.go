package handler

import (
	"net/http"
	"strconv"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared/httputil"

	"github.com/go-chi/chi/v5"
)

type featuredTracksResponse struct {
	Items []TrackResponse `json:"items"`
	Total int             `json:"total"`
}

type FeaturedArtistHandler struct {
	backfillFeatured *service.BackfillFeaturedService
	listFeaturing    *service.ListFeaturingService
}

func NewFeaturedArtistHandler(
	backfillFeatured *service.BackfillFeaturedService,
	listFeaturing *service.ListFeaturingService,
) *FeaturedArtistHandler {
	return &FeaturedArtistHandler{
		backfillFeatured: backfillFeatured,
		listFeaturing:    listFeaturing,
	}
}

// Routes registers the featured-artist endpoints on r. Like StreamHandler and
// AudioURLHandler it registers directly onto a shared router; here the router is
// TrackHandler's, because these paths are deliberately composed into the /tracks
// surface rather than mounted under a prefix of their own.
func (h *FeaturedArtistHandler) Routes(r chi.Router) {
	r.Get("/featuring", h.handleListFeaturing)
	r.Post("/featured-backfill", h.handleBackfillFeatured)
}

func (h *FeaturedArtistHandler) handleBackfillFeatured(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	result, err := h.backfillFeatured.Execute(r.Context(), userId)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, result)
}

func (h *FeaturedArtistHandler) handleListFeaturing(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	name := q.Get("name")
	mbid := q.Get("mbid")
	var deezerID int64
	if v := q.Get("deezer_id"); v != "" {
		deezerID, _ = strconv.ParseInt(v, 10, 64)
	}
	if name == "" && mbid == "" && deezerID == 0 {
		httputil.BadRequest(w, "one of name, mbid, or deezer_id is required")
		return
	}

	fa := domain.FeaturedArtistForQuery(name, mbid, deezerID)

	tracks, err := h.listFeaturing.Execute(r.Context(), userId, fa)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	items := tracksToDTO(tracks)
	httputil.WriteJSON(w, http.StatusOK, featuredTracksResponse{Items: items, Total: len(items)})
}
