package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"altune/go-api/internal/auth"
	acqService "altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type TrackHandler struct {
	addTrack    *service.AddTrackService
	listTracks  *service.ListTracksService
	deleteTrack *service.DeleteTrackService
	reconcile   *service.ReconcileTrackStatusService
	acquireSvc  *acqService.AcquireTrackAudioService
	wg          *sync.WaitGroup
	sem         chan struct{}
}

func NewTrackHandler(
	addTrack *service.AddTrackService,
	listTracks *service.ListTracksService,
	deleteTrack *service.DeleteTrackService,
	reconcile *service.ReconcileTrackStatusService,
	acquireSvc *acqService.AcquireTrackAudioService,
	wg *sync.WaitGroup,
	sem chan struct{},
) *TrackHandler {
	return &TrackHandler{
		addTrack:    addTrack,
		listTracks:  listTracks,
		deleteTrack: deleteTrack,
		reconcile:   reconcile,
		acquireSvc:  acquireSvc,
		wg:          wg,
		sem:         sem,
	}
}

func (h *TrackHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.handleListTracks)
	r.Post("/", h.handleCreateTrack)
	r.Delete("/{trackId}", h.handleDeleteTrack)
	return r
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
}

type TrackResponse struct {
	ID                uuid.UUID `json:"id"`
	Title             string    `json:"title"`
	Artist            string    `json:"artist"`
	Album             *string   `json:"album"`
	DurationSeconds   *float64  `json:"duration_seconds"`
	AddedAt           time.Time `json:"added_at"`
	AcquisitionStatus string    `json:"acquisition_status"`
	ArtworkURL        *string   `json:"artwork_url"`
	Year              *int      `json:"year,omitempty"`
	Genre             *string   `json:"genre,omitempty"`
	TrackNumber       *int      `json:"track_number,omitempty"`
	AlbumArtist       *string   `json:"album_artist,omitempty"`
	ISRC              *string   `json:"isrc,omitempty"`
	AudioRef          *string   `json:"audio_ref,omitempty"`
	FailureReason     *string   `json:"failure_reason,omitempty"`
}

type ListTracksResponse struct {
	Items   []TrackResponse `json:"items"`
	Total   int             `json:"total"`
	Limit   int             `json:"limit"`
	Offset  int             `json:"offset"`
	HasMore bool            `json:"has_more"`
}

func trackToResponse(t *domain.Track) TrackResponse {
	var album *string
	if t.Album != "" {
		album = &t.Album
	}
	return TrackResponse{
		ID:                t.ID.UUID(),
		Title:             t.Title,
		Artist:            t.Artist,
		Album:             album,
		DurationSeconds:   t.DurationSeconds,
		AddedAt:           t.AddedAt,
		AcquisitionStatus: t.AcquisitionStatus.String(),
		ArtworkURL:        t.ArtworkURL,
		Year:              t.Year,
		Genre:             t.Genre,
		TrackNumber:       t.TrackNumber,
		AlbumArtist:       t.AlbumArtist,
		ISRC:              t.ISRC,
		AudioRef:          t.AudioRef,
		FailureReason:     t.FailureReason,
	}
}

// --- Handlers ---

func (h *TrackHandler) handleListTracks(w http.ResponseWriter, r *http.Request) {
	userId := auth.MustUserID(r.Context())

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 50
	}

	result, err := h.listTracks.Execute(r.Context(), userId, limit, offset)
	if err != nil {
		httputil.InternalError(w)
		return
	}

	items := make([]TrackResponse, len(result.Tracks))
	for i, t := range result.Tracks {
		items[i] = trackToResponse(t)
	}

	httputil.WriteJSON(w, http.StatusOK, ListTracksResponse{
		Items:   items,
		Total:   result.Total,
		Limit:   limit,
		Offset:  offset,
		HasMore: result.HasMore,
	})
}

func (h *TrackHandler) handleCreateTrack(w http.ResponseWriter, r *http.Request) {
	userId := auth.MustUserID(r.Context())

	var req CreateTrackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "invalid request body")
		return
	}

	if req.Title == "" || req.Artist == "" {
		httputil.BadRequest(w, "title and artist are required")
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
	}

	result, err := h.addTrack.Execute(r.Context(), userId, input)
	if err != nil {
		httputil.InternalError(w)
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

	slog.InfoContext(r.Context(), "acquisition.scheduled",
		"track_id", result.Track.ID.String())
	h.scheduleAcquisition(userId, result.Track.ID)

	httputil.WriteJSON(w, status, trackToResponse(result.Track))
}

func (h *TrackHandler) handleDeleteTrack(w http.ResponseWriter, r *http.Request) {
	userId := auth.MustUserID(r.Context())

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
		if err == service.ErrTrackNotFound {
			httputil.NotFound(w, "track not found")
			return
		}
		httputil.InternalError(w)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *TrackHandler) scheduleAcquisition(userId shared.UserId, trackId domain.TrackId) {
	if h.acquireSvc == nil {
		slog.Warn("acquisition_not_configured")
		return
	}

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()

		h.sem <- struct{}{}
		defer func() { <-h.sem }()

		bgCtx := context.Background()
		if err := h.acquireSvc.Execute(bgCtx, userId, trackId); err != nil {
			slog.Error("background acquisition failed",
				"track_id", trackId.String(), "error", err)
		}
	}()
}
