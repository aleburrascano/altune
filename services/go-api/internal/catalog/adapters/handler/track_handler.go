package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared/httputil"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type TrackHandler struct {
	addTrack         *service.AddTrackService
	listTracks       *service.ListTracksService
	getTrackStatus   *service.GetTrackStatusService
	deleteTrack      *service.DeleteTrackService
	setTrackNumber   *service.SetTrackNumberService
	backfillFeatured *service.BackfillFeaturedService
	listFeaturing    *service.ListFeaturingService
}

func NewTrackHandler(
	addTrack *service.AddTrackService,
	listTracks *service.ListTracksService,
	getTrackStatus *service.GetTrackStatusService,
	deleteTrack *service.DeleteTrackService,
	setTrackNumber *service.SetTrackNumberService,
	backfillFeatured *service.BackfillFeaturedService,
	listFeaturing *service.ListFeaturingService,
) *TrackHandler {
	return &TrackHandler{
		addTrack:         addTrack,
		listTracks:       listTracks,
		getTrackStatus:   getTrackStatus,
		deleteTrack:      deleteTrack,
		setTrackNumber:   setTrackNumber,
		backfillFeatured: backfillFeatured,
		listFeaturing:    listFeaturing,
	}
}

func (h *TrackHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.handleListTracks)
	r.Post("/", h.handleCreateTrack)
	r.Get("/featuring", h.handleListFeaturing)
	r.Post("/featured-backfill", h.handleBackfillFeatured)
	r.Get("/{trackId}/status", h.handleGetTrackStatus)
	r.Patch("/{trackId}/track-number", h.handleSetTrackNumber)
	r.Delete("/{trackId}", h.handleDeleteTrack)
	return r
}

// handleSetTrackNumber persists a track's album position (fill-only — never
// overwrites). Backs the client persisting positions it derived from the album
// tracklist for tracks saved before track_number was captured.
func (h *TrackHandler) handleSetTrackNumber(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	trackId, err := domain.ParseTrackId(chi.URLParam(r, "trackId"))
	if err != nil {
		httputil.BadRequest(w, "invalid track ID")
		return
	}
	var req SetTrackNumberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "invalid request body")
		return
	}
	if _, err := h.setTrackNumber.Execute(r.Context(), userId, trackId, req.TrackNumber); err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type SetTrackNumberRequest struct {
	TrackNumber int `json:"track_number"`
}

// handleBackfillFeatured resolves and persists featured artists for the authed
// user's existing tracks (idempotent). Synchronous — the library is small.
func (h *TrackHandler) handleBackfillFeatured(w http.ResponseWriter, r *http.Request) {
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
func (h *TrackHandler) handleListFeaturing(w http.ResponseWriter, r *http.Request) {
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

// --- DTOs ---

type CreateTrackRequest struct {
	Title           string   `json:"title"`
	Artist          string   `json:"artist"`
	Album           *string  `json:"album,omitempty"`
	DurationSeconds *float64 `json:"duration_seconds,omitempty"`
	ArtworkURL      *string  `json:"artwork_url,omitempty"`
	ISRC            *string  `json:"isrc,omitempty"`
	Year            *int     `json:"year,omitempty"`
	Genre           *string  `json:"genre,omitempty"`
	AlbumArtist     *string  `json:"album_artist,omitempty"`
	// FeaturedArtists are the guest ("feat.") credits carried from the discovery
	// result the client saved, persisted on the track.
	FeaturedArtists []FeaturedArtistDTO `json:"featured_artists,omitempty"`
	// SourceURL is the exact provider URL the saved result was discovered at
	// (e.g. a SoundCloud permalink). When it is a directly-downloadable source,
	// acquisition grabs that exact track instead of re-searching by metadata.
	// Not persisted — it rides through to the acquisition scheduler only.
	SourceURL *string `json:"source_url,omitempty"`
}

// TrackResponse is the track wire shape, owned by the service layer so the
// track_added_to_library event payload and this HTTP response can never drift —
// they serialize the same struct (service.TrackDTO).
type TrackResponse = service.TrackDTO

type ListTracksResponse struct {
	Items   []TrackResponse `json:"items"`
	Total   int             `json:"total"`
	Limit   int             `json:"limit"`
	Offset  int             `json:"offset"`
	HasMore bool            `json:"has_more"`
}

// --- Handlers ---

func (h *TrackHandler) handleListTracks(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	result, err := h.listTracks.Execute(r.Context(), userId, limit, offset)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	items := make([]TrackResponse, len(result.Tracks))
	for i, t := range result.Tracks {
		items[i] = service.TrackToDTO(t)
	}

	httputil.WriteJSON(w, http.StatusOK, ListTracksResponse{
		Items:   items,
		Total:   result.Total,
		Limit:   result.Limit,
		Offset:  offset,
		HasMore: result.HasMore,
	})
}

func (h *TrackHandler) handleCreateTrack(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	var req CreateTrackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "invalid request body")
		return
	}

	album := ""
	if req.Album != nil {
		album = *req.Album
	}

	input := service.AddTrackInput{
		Title:           req.Title,
		Artist:          req.Artist,
		Album:           album,
		DurationSeconds: req.DurationSeconds,
		ArtworkURL:      req.ArtworkURL,
		Year:            req.Year,
		Genre:           req.Genre,
		ISRC:            req.ISRC,
		AlbumArtist:     req.AlbumArtist,
		FeaturedArtists: domainFeaturedFromDTOs(req.FeaturedArtists),
		SourceURL:       req.SourceURL,
	}

	// Validation lives in the domain (NewTrack returns a coded 400) — no
	// duplicated pre-checks here; HandleServiceError renders the status.
	result, err := h.addTrack.Execute(r.Context(), userId, input)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
		slog.InfoContext(r.Context(), "track.saved",
			"track_id", result.Track.ID.String(),
			"title", result.Track.Title,
			"artist", result.Track.Artist,
			"album", result.Track.Album,
		)
	} else {
		slog.InfoContext(r.Context(), "track.dedup_hit",
			"track_id", result.Track.ID.String(),
			"title", result.Track.Title,
			"status", result.Track.AcquisitionStatus.String(),
		)
	}

	httputil.WriteJSON(w, status, service.TrackToDTO(result.Track))
}

type TrackStatusResponse struct {
	ID                uuid.UUID `json:"id"`
	AcquisitionStatus string    `json:"acquisition_status"`
	FailureReason     *string   `json:"failure_reason,omitempty"`
}

func (h *TrackHandler) handleGetTrackStatus(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	trackId, err := domain.ParseTrackId(chi.URLParam(r, "trackId"))
	if err != nil {
		httputil.BadRequest(w, "invalid track ID")
		return
	}

	track, err := h.getTrackStatus.Execute(r.Context(), userId, trackId)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}
	if track == nil {
		httputil.NotFound(w, "track not found")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, TrackStatusResponse{
		ID:                track.ID.UUID(),
		AcquisitionStatus: track.AcquisitionStatus.String(),
		FailureReason:     track.FailureReason,
	})
}

func (h *TrackHandler) handleDeleteTrack(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	trackIdStr := chi.URLParam(r, "trackId")
	trackId, err := domain.ParseTrackId(trackIdStr)
	if err != nil {
		httputil.BadRequest(w, "invalid track ID")
		return
	}

	slog.InfoContext(r.Context(), "track.delete",
		"track_id", trackId.String())

	err = h.deleteTrack.Execute(r.Context(), userId, trackId)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
