package handler

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/service"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/requestuser"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type PlaylistHandler struct {
	lifecycle    *service.PlaylistLifecycleService
	membership   *service.PlaylistMembershipService
	now          func() time.Time
	writeLimiter rateLimitMiddleware
}

func NewPlaylistHandler(lifecycle *service.PlaylistLifecycleService, membership *service.PlaylistMembershipService, now clock.Func, opts ...func(*PlaylistHandler)) (*PlaylistHandler, error) {
	if now == nil {
		return nil, fmt.Errorf("NewPlaylistHandler: %w", domain.ErrHandlerClockRequired)
	}
	h := &PlaylistHandler{
		lifecycle:  lifecycle,
		membership: membership,
		now:        now,
	}
	for _, opt := range opts {
		opt(h)
	}
	h.writeLimiter = newWriteRateLimiter(playlistWriteRateLimit, h.now)
	return h, nil
}

func MustNewPlaylistHandler(lifecycle *service.PlaylistLifecycleService, membership *service.PlaylistMembershipService, now clock.Func, opts ...func(*PlaylistHandler)) *PlaylistHandler {
	h, err := NewPlaylistHandler(lifecycle, membership, now, opts...)
	if err != nil {
		panic(err)
	}
	return h
}

func trackIdsFromUUIDs(ids []uuid.UUID) []domain.TrackId {
	trackIds := make([]domain.TrackId, len(ids))
	for i, id := range ids {
		trackIds[i] = domain.TrackIdFromUUID(id)
	}
	return trackIds
}

func (h *PlaylistHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	var req CreatePlaylistRequest
	if !httpserver.DecodeJSON(w, r, &req) {
		return
	}

	playlist, created, err := h.lifecycle.Create(r.Context(), userId, req.Name, httpserver.IdempotencyKey(r))
	if err != nil {
		HandleError(w, r, err)
		return
	}

	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	httpserver.WriteJSON(w, status, playlistToResponse(playlist, 0, nil))
}

func (h *PlaylistHandler) handleList(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	limit, offset, err := pageBounds(r)
	if err != nil {
		HandleError(w, r, err)
		return
	}
	playlists, err := h.lifecycle.List(r.Context(), userId, limit, offset)
	if err != nil {
		HandleError(w, r, err)
		return
	}

	items := httpserver.MapSlice(playlists, func(ps domain.PlaylistWithSummary) PlaylistResponse {
		return playlistToResponse(ps.Playlist, ps.Summary.TrackCount, ps.Summary.PreviewArtworkURLs)
	})

	httpserver.WriteJSON(w, http.StatusOK, httpserver.NewList(items))
}

func (h *PlaylistHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)
	playlistId, ok := httpserver.PathID(w, r, "playlistId", domain.ParsePlaylistId, "invalid playlist ID")
	if !ok {
		return
	}

	playlist, tracks, err := h.lifecycle.Get(r.Context(), userId, playlistId)
	if err != nil {
		HandleError(w, r, err)
		return
	}

	trackResponses := tracksToDTO(tracks)

	artworkURLs := domain.PreviewArtworkURLs(tracks)

	httpserver.WriteJSON(w, http.StatusOK, PlaylistDetailResponse{
		PlaylistResponse:     playlistToResponse(playlist, len(tracks), artworkURLs),
		TotalDurationSeconds: domain.TotalDurationSeconds(tracks),
		Tracks:               trackResponses,
	})
}

func (h *PlaylistHandler) handleRename(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)
	playlistId, ok := httpserver.PathID(w, r, "playlistId", domain.ParsePlaylistId, "invalid playlist ID")
	if !ok {
		return
	}

	var req RenamePlaylistRequest
	if !httpserver.DecodeJSON(w, r, &req) {
		return
	}

	playlist, summary, err := h.lifecycle.Rename(r.Context(), userId, playlistId, req.Name)
	if err != nil {
		HandleError(w, r, err)
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, playlistToResponse(playlist, summary.TrackCount, summary.PreviewArtworkURLs))
}

func (h *PlaylistHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)
	playlistId, ok := httpserver.PathID(w, r, "playlistId", domain.ParsePlaylistId, "invalid playlist ID")
	if !ok {
		return
	}

	if err := h.lifecycle.Delete(r.Context(), userId, playlistId); err != nil {
		HandleError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *PlaylistHandler) handleAddTrack(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)
	playlistId, ok := httpserver.PathID(w, r, "playlistId", domain.ParsePlaylistId, "invalid playlist ID")
	if !ok {
		return
	}

	var req AddTrackToPlaylistRequest
	if !httpserver.DecodeJSON(w, r, &req) {
		return
	}

	trackId := domain.TrackIdFromUUID(req.TrackID)
	if err := h.membership.AddTrack(r.Context(), userId, playlistId, trackId); err != nil {
		HandleError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *PlaylistHandler) handleAddTracks(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)
	playlistId, ok := httpserver.PathID(w, r, "playlistId", domain.ParsePlaylistId, "invalid playlist ID")
	if !ok {
		return
	}

	var req AddTracksToPlaylistRequest
	if !httpserver.DecodeJSON(w, r, &req) {
		return
	}
	if len(req.TrackIDs) == 0 {
		HandleError(w, r, domain.ErrTrackIDsRequired)
		return
	}

	trackIds := trackIdsFromUUIDs(req.TrackIDs)

	added, err := h.membership.AddTracks(r.Context(), userId, playlistId, trackIds)
	if err != nil {
		HandleError(w, r, err)
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, AddTracksToPlaylistResponse{
		Added:   added,
		Skipped: len(trackIds) - added,
	})
}

func (h *PlaylistHandler) handleRemoveTrack(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)
	playlistId, ok := httpserver.PathID(w, r, "playlistId", domain.ParsePlaylistId, "invalid playlist ID")
	if !ok {
		return
	}
	trackId, ok := httpserver.PathID(w, r, "trackId", domain.ParseTrackId, "invalid track ID")
	if !ok {
		return
	}

	if err := h.membership.RemoveTrack(r.Context(), userId, playlistId, trackId); err != nil {
		HandleError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *PlaylistHandler) handleRemoveTracks(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)
	playlistId, ok := httpserver.PathID(w, r, "playlistId", domain.ParsePlaylistId, "invalid playlist ID")
	if !ok {
		return
	}

	var req RemoveTracksFromPlaylistRequest
	if !httpserver.DecodeJSON(w, r, &req) {
		return
	}
	if len(req.TrackIDs) == 0 {
		HandleError(w, r, domain.ErrTrackIDsRequired)
		return
	}

	trackIds := trackIdsFromUUIDs(req.TrackIDs)

	removed, err := h.membership.RemoveTracks(r.Context(), userId, playlistId, trackIds)
	if err != nil {
		HandleError(w, r, err)
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, RemoveTracksFromPlaylistResponse{Removed: removed})
}

func (h *PlaylistHandler) handleReorder(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)
	playlistId, ok := httpserver.PathID(w, r, "playlistId", domain.ParsePlaylistId, "invalid playlist ID")
	if !ok {
		return
	}

	var req ReorderTracksRequest
	if !httpserver.DecodeJSON(w, r, &req) {
		return
	}

	trackIds := trackIdsFromUUIDs(req.TrackIDs)

	if err := h.membership.Reorder(r.Context(), userId, playlistId, trackIds); err != nil {
		HandleError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
