package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared/httputil"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type PlaylistHandler struct {
	lifecycle    *service.PlaylistLifecycleService
	membership   *service.PlaylistMembershipService
	writeLimit   AudioRateLimit
	now          func() time.Time
	writeLimiter *audioRateLimiter
}

func NewPlaylistHandler(lifecycle *service.PlaylistLifecycleService, membership *service.PlaylistMembershipService, opts ...func(*PlaylistHandler)) *PlaylistHandler {
	h := &PlaylistHandler{
		lifecycle:  lifecycle,
		membership: membership,
		writeLimit: DefaultPlaylistWriteRateLimit,
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(h)
	}
	h.writeLimiter = newWriteRateLimiter(h.writeLimit, h.now)
	return h
}

// WithPlaylistWriteRateLimit replaces DefaultPlaylistWriteRateLimit.
func WithPlaylistWriteRateLimit(limit AudioRateLimit) func(*PlaylistHandler) {
	return func(h *PlaylistHandler) { h.writeLimit = limit }
}

// withPlaylistWriteClock injects the limiter's clock so tests can refill
// buckets without sleeping.
func withPlaylistWriteClock(now func() time.Time) func(*PlaylistHandler) {
	return func(h *PlaylistHandler) { h.now = now }
}

// Routes registers the playlist endpoints. The three routes that create rows —
// a playlist, and the two that insert memberships — share one per-user bucket,
// because they grow the same account the same way; removals, renames and reads
// are left unthrottled, as none of them can accumulate.
func (h *PlaylistHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(h.writeLimiter.middleware)
		r.Post("/", h.handleCreate)
		r.Post("/{playlistId}/tracks", h.handleAddTrack)
		r.Post("/{playlistId}/tracks/batch", h.handleAddTracks)
	})
	r.Get("/", h.handleList)
	r.Get("/{playlistId}", h.handleGet)
	r.Patch("/{playlistId}", h.handleRename)
	r.Delete("/{playlistId}", h.handleDelete)
	r.Delete("/{playlistId}/tracks/{trackId}", h.handleRemoveTrack)
	r.Delete("/{playlistId}/tracks", h.handleRemoveTracks)
	r.Patch("/{playlistId}/tracks/reorder", h.handleReorder)
	return r
}

type CreatePlaylistRequest struct {
	Name string `json:"name"`
}

type RenamePlaylistRequest struct {
	Name string `json:"name"`
}

type AddTrackToPlaylistRequest struct {
	TrackID uuid.UUID `json:"track_id"`
}

type AddTracksToPlaylistRequest struct {
	TrackIDs []uuid.UUID `json:"track_ids"`
}

type AddTracksToPlaylistResponse struct {
	Added   int `json:"added"`
	Skipped int `json:"skipped"`
}

type RemoveTracksFromPlaylistRequest struct {
	TrackIDs []uuid.UUID `json:"track_ids"`
}

type RemoveTracksFromPlaylistResponse struct {
	Removed int `json:"removed"`
}

type ReorderTracksRequest struct {
	TrackIDs []uuid.UUID `json:"track_ids"`
}

type PlaylistResponse struct {
	ID                 uuid.UUID `json:"id"`
	Name               string    `json:"name"`
	TrackCount         int       `json:"track_count"`
	PreviewArtworkURLs []string  `json:"preview_artwork_urls"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type PlaylistDetailResponse struct {
	PlaylistResponse
	TotalDurationSeconds float64         `json:"total_duration_seconds"`
	Tracks               []TrackResponse `json:"tracks"`
}

func trackIdsFromUUIDs(ids []uuid.UUID) []domain.TrackId {
	trackIds := make([]domain.TrackId, len(ids))
	for i, id := range ids {
		trackIds[i] = domain.TrackIdFromUUID(id)
	}
	return trackIds
}

func playlistToResponse(p *domain.Playlist, trackCount int, artworkURLs []string) PlaylistResponse {
	if artworkURLs == nil {
		artworkURLs = []string{}
	}
	return PlaylistResponse{
		ID:                 p.ID.UUID(),
		Name:               p.Name,
		TrackCount:         trackCount,
		PreviewArtworkURLs: artworkURLs,
		CreatedAt:          p.CreatedAt,
		UpdatedAt:          p.UpdatedAt,
	}
}

func (h *PlaylistHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	var req CreatePlaylistRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	playlist, err := h.lifecycle.Create(r.Context(), userId, req.Name)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, playlistToResponse(playlist, 0, nil))
}

func (h *PlaylistHandler) handleList(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	limit, offset := pageBounds(r)
	playlists, err := h.lifecycle.List(r.Context(), userId, limit, offset)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	items := httputil.MapSlice(playlists, func(ps domain.PlaylistWithSummary) PlaylistResponse {
		return playlistToResponse(ps.Playlist, ps.Summary.TrackCount, ps.Summary.PreviewArtworkURLs)
	})

	httputil.WriteJSON(w, http.StatusOK, httputil.NewList(items))
}

func (h *PlaylistHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	playlistId, ok := httputil.PathID(w, r, "playlistId", domain.ParsePlaylistId, "invalid playlist ID")
	if !ok {
		return
	}

	playlist, tracks, err := h.lifecycle.Get(r.Context(), userId, playlistId)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	trackResponses := tracksToDTO(tracks)

	artworkURLs := domain.PreviewArtworkURLs(tracks)

	httputil.WriteJSON(w, http.StatusOK, PlaylistDetailResponse{
		PlaylistResponse:     playlistToResponse(playlist, len(tracks), artworkURLs),
		TotalDurationSeconds: domain.TotalDurationSeconds(tracks),
		Tracks:               trackResponses,
	})
}

func (h *PlaylistHandler) handleRename(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	playlistId, ok := httputil.PathID(w, r, "playlistId", domain.ParsePlaylistId, "invalid playlist ID")
	if !ok {
		return
	}

	var req RenamePlaylistRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	playlist, summary, err := h.lifecycle.Rename(r.Context(), userId, playlistId, req.Name)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, playlistToResponse(playlist, summary.TrackCount, summary.PreviewArtworkURLs))
}

func (h *PlaylistHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	playlistId, ok := httputil.PathID(w, r, "playlistId", domain.ParsePlaylistId, "invalid playlist ID")
	if !ok {
		return
	}

	if err := h.lifecycle.Delete(r.Context(), userId, playlistId); err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *PlaylistHandler) handleAddTrack(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	playlistId, ok := httputil.PathID(w, r, "playlistId", domain.ParsePlaylistId, "invalid playlist ID")
	if !ok {
		return
	}

	var req AddTrackToPlaylistRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	trackId := domain.TrackIdFromUUID(req.TrackID)
	if err := h.membership.AddTrack(r.Context(), userId, playlistId, trackId); err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *PlaylistHandler) handleAddTracks(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	playlistId, ok := httputil.PathID(w, r, "playlistId", domain.ParsePlaylistId, "invalid playlist ID")
	if !ok {
		return
	}

	var req AddTracksToPlaylistRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}
	if len(req.TrackIDs) == 0 {
		httputil.HandleServiceError(w, r, domain.ErrTrackIDsRequired)
		return
	}

	trackIds := trackIdsFromUUIDs(req.TrackIDs)

	added, err := h.membership.AddTracks(r.Context(), userId, playlistId, trackIds)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, AddTracksToPlaylistResponse{
		Added:   added,
		Skipped: len(trackIds) - added,
	})
}

func (h *PlaylistHandler) handleRemoveTrack(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	playlistId, ok := httputil.PathID(w, r, "playlistId", domain.ParsePlaylistId, "invalid playlist ID")
	if !ok {
		return
	}
	trackId, ok := httputil.PathID(w, r, "trackId", domain.ParseTrackId, "invalid track ID")
	if !ok {
		return
	}

	if err := h.membership.RemoveTrack(r.Context(), userId, playlistId, trackId); err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *PlaylistHandler) handleRemoveTracks(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	playlistId, ok := httputil.PathID(w, r, "playlistId", domain.ParsePlaylistId, "invalid playlist ID")
	if !ok {
		return
	}

	var req RemoveTracksFromPlaylistRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}
	if len(req.TrackIDs) == 0 {
		httputil.HandleServiceError(w, r, domain.ErrTrackIDsRequired)
		return
	}

	trackIds := trackIdsFromUUIDs(req.TrackIDs)

	removed, err := h.membership.RemoveTracks(r.Context(), userId, playlistId, trackIds)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, RemoveTracksFromPlaylistResponse{Removed: removed})
}

func (h *PlaylistHandler) handleReorder(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	playlistId, ok := httputil.PathID(w, r, "playlistId", domain.ParsePlaylistId, "invalid playlist ID")
	if !ok {
		return
	}

	var req ReorderTracksRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	trackIds := trackIdsFromUUIDs(req.TrackIDs)

	if err := h.membership.Reorder(r.Context(), userId, playlistId, trackIds); err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
