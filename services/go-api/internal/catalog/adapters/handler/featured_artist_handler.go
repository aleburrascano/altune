package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared/httputil"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

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

// Routes registers the featured-artist endpoints on r. Like StreamHandler and
// AudioURLHandler it registers directly onto a shared router; here the router is
// TrackHandler's, because these paths are deliberately composed into the /tracks
// surface rather than mounted under a prefix of their own.
func (h *FeaturedArtistHandler) Routes(r chi.Router) {
	r.Get("/featuring", h.handleListFeaturing)
	r.Post("/featured-backfill", h.handleBackfillFeatured)
}

// handleBackfillFeatured scans from the ?offset= the caller names (absent or
// unparseable means the start of the library). A response with truncated=true
// carries the next_offset a follow-up call passes back here to continue.
func (h *FeaturedArtistHandler) handleBackfillFeatured(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
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

// logPartialBackfill keeps the work a failed run already committed visible: the
// error response has no room for counts, so without this line the tracks
// resolved before the failure, and the offset a retry resumes from, are lost.
func logPartialBackfill(r *http.Request, result *service.BackfillFeaturedResult) {
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
	userId, ok := auth.RequireUserID(w, r)
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

// parseDeezerID rejects rather than drops a deezer_id it cannot read: a
// dropped one either answers 200 for an artist the caller never asked about,
// or falls through to a "required" 400 that blames the wrong parameter.
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
