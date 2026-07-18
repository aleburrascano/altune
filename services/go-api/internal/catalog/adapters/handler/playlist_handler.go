package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared/httputil"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type PlaylistHandler struct {
	svc *service.PlaylistService
}

func NewPlaylistHandler(svc *service.PlaylistService) *PlaylistHandler {
	return &PlaylistHandler{svc: svc}
}

func (h *PlaylistHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.handleCreate)
	r.Get("/", h.handleList)
	r.Get("/{playlistId}", h.handleGet)
	r.Patch("/{playlistId}", h.handleRename)
	r.Delete("/{playlistId}", h.handleDelete)
	r.Post("/{playlistId}/tracks", h.handleAddTrack)
	r.Delete("/{playlistId}/tracks/{trackId}", h.handleRemoveTrack)
	r.Patch("/{playlistId}/tracks/reorder", h.handleReorder)
	return r
}

// --- DTOs ---

type CreatePlaylistRequest struct {
	Name string `json:"name"`
}

type RenamePlaylistRequest struct {
	Name string `json:"name"`
}

type AddTrackToPlaylistRequest struct {
	TrackID uuid.UUID `json:"track_id"`
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

type ListPlaylistsResponse struct {
	Items []PlaylistResponse `json:"items"`
	Total int                `json:"total"`
}

type PlaylistDetailResponse struct {
	ID                 uuid.UUID       `json:"id"`
	Name               string          `json:"name"`
	TrackCount         int             `json:"track_count"`
	PreviewArtworkURLs []string        `json:"preview_artwork_urls"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	Tracks             []TrackResponse `json:"tracks"`
}

// playlistToResponse maps a playlist summary to its wire DTO — one mapper for
// the create/list/rename responses (the detail response carries tracks too).
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


// --- Handlers ---

func (h *PlaylistHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	var req CreatePlaylistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "invalid request body")
		return
	}

	playlist, err := h.svc.Create(r.Context(), userId, req.Name)
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

	playlists, err := h.svc.List(r.Context(), userId)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	items := make([]PlaylistResponse, len(playlists))
	for i, p := range playlists {
		// Track count and preview artwork are projections the list query
		// already computed — no per-playlist follow-up queries.
		items[i] = playlistToResponse(p, p.TrackCount, p.PreviewArtworkURLs)
	}

	httputil.WriteJSON(w, http.StatusOK, ListPlaylistsResponse{
		Items: items,
		Total: len(items),
	})
}

func (h *PlaylistHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	playlistId, err := domain.ParsePlaylistId(chi.URLParam(r, "playlistId"))
	if err != nil {
		httputil.BadRequest(w, "invalid playlist ID")
		return
	}

	playlist, tracks, err := h.svc.Get(r.Context(), userId, playlistId)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	trackResponses := make([]TrackResponse, len(tracks))
	for i, t := range tracks {
		trackResponses[i] = service.TrackToDTO(t)
	}

	artworkURLs := service.PreviewArtworkURLs(tracks)

	httputil.WriteJSON(w, http.StatusOK, PlaylistDetailResponse{
		ID:                 playlist.ID.UUID(),
		Name:               playlist.Name,
		TrackCount:         len(tracks),
		PreviewArtworkURLs: artworkURLs,
		CreatedAt:          playlist.CreatedAt,
		UpdatedAt:          playlist.UpdatedAt,
		Tracks:             trackResponses,
	})
}

func (h *PlaylistHandler) handleRename(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	playlistId, err := domain.ParsePlaylistId(chi.URLParam(r, "playlistId"))
	if err != nil {
		httputil.BadRequest(w, "invalid playlist ID")
		return
	}

	var req RenamePlaylistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "invalid request body")
		return
	}

	playlist, err := h.svc.Rename(r.Context(), userId, playlistId, req.Name)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	// TrackCount is the GetByID projection — playlist.Tracks is never loaded on
	// this path, so len(playlist.Tracks) would always report 0.
	httputil.WriteJSON(w, http.StatusOK, playlistToResponse(playlist, playlist.TrackCount, nil))
}

func (h *PlaylistHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	playlistId, err := domain.ParsePlaylistId(chi.URLParam(r, "playlistId"))
	if err != nil {
		httputil.BadRequest(w, "invalid playlist ID")
		return
	}

	if err := h.svc.Delete(r.Context(), userId, playlistId); err != nil {
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
	playlistId, err := domain.ParsePlaylistId(chi.URLParam(r, "playlistId"))
	if err != nil {
		httputil.BadRequest(w, "invalid playlist ID")
		return
	}

	var req AddTrackToPlaylistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "invalid request body")
		return
	}

	trackId := domain.TrackIdFromUUID(req.TrackID)
	if err := h.svc.AddTrack(r.Context(), userId, playlistId, trackId); err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *PlaylistHandler) handleRemoveTrack(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	playlistId, err := domain.ParsePlaylistId(chi.URLParam(r, "playlistId"))
	if err != nil {
		httputil.BadRequest(w, "invalid playlist ID")
		return
	}
	trackId, err := domain.ParseTrackId(chi.URLParam(r, "trackId"))
	if err != nil {
		httputil.BadRequest(w, "invalid track ID")
		return
	}

	if err := h.svc.RemoveTrack(r.Context(), userId, playlistId, trackId); err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *PlaylistHandler) handleReorder(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	playlistId, err := domain.ParsePlaylistId(chi.URLParam(r, "playlistId"))
	if err != nil {
		httputil.BadRequest(w, "invalid playlist ID")
		return
	}

	var req ReorderTracksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "invalid request body")
		return
	}

	trackIds := make([]domain.TrackId, len(req.TrackIDs))
	for i, id := range req.TrackIDs {
		trackIds[i] = domain.TrackIdFromUUID(id)
	}

	if err := h.svc.Reorder(r.Context(), userId, playlistId, trackIds); err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
