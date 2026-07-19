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

// AddRoutes registers the featured-artist routes onto an existing chi.Router.
// This handler shares the /tracks prefix with TrackHandler, so the caller is
// responsible for mounting both onto the same subrouter.
func (h *FeaturedArtistHandler) AddRoutes(r chi.Router) {
	r.Get("/featuring", h.handleListFeaturing)
	r.Post("/featured-backfill", h.handleBackfillFeatured)
}

// handleBackfillFeatured resolves and persists featured artists for the authed
// user's existing tracks (idempotent). Synchronous — the library is small.
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

// handleListFeaturing returns the user's tracks crediting a featured artist,
// identified by mbid, deezer_id, or name (in that precedence).
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

	items := make([]TrackResponse, len(tracks))
	for i, t := range tracks {
		items[i] = service.TrackToDTO(t)
	}
	httputil.WriteJSON(w, http.StatusOK, ListTracksResponse{
		Items: items, Total: len(items), Limit: len(items), Offset: 0, HasMore: false,
	})
}
