package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared/httputil"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type TrackHandler struct {
	addTrack       *service.AddTrackService
	listTracks     *service.ListTracksService
	getTrackStatus *service.GetTrackStatusService
	deleteTrack    *service.DeleteTrackService
	setTrackNumber *service.SetTrackNumberService
	featuredArtist *FeaturedArtistHandler
	writeLimit     AudioRateLimit
	now            func() time.Time
	writeLimiter   *audioRateLimiter
}

func NewTrackHandler(
	addTrack *service.AddTrackService,
	listTracks *service.ListTracksService,
	getTrackStatus *service.GetTrackStatusService,
	deleteTrack *service.DeleteTrackService,
	setTrackNumber *service.SetTrackNumberService,
	featuredArtist *FeaturedArtistHandler,
	opts ...func(*TrackHandler),
) *TrackHandler {
	h := &TrackHandler{
		addTrack:       addTrack,
		listTracks:     listTracks,
		getTrackStatus: getTrackStatus,
		deleteTrack:    deleteTrack,
		setTrackNumber: setTrackNumber,
		featuredArtist: featuredArtist,
		writeLimit:     DefaultTrackWriteRateLimit,
		now:            time.Now,
	}
	for _, opt := range opts {
		opt(h)
	}
	h.writeLimiter = newWriteRateLimiter(h.writeLimit, h.now)
	return h
}

// WithTrackWriteRateLimit replaces DefaultTrackWriteRateLimit.
func WithTrackWriteRateLimit(limit AudioRateLimit) func(*TrackHandler) {
	return func(h *TrackHandler) { h.writeLimit = limit }
}

// withTrackWriteClock injects the limiter's clock so tests can refill buckets
// without sleeping.
func withTrackWriteClock(now func() time.Time) func(*TrackHandler) {
	return func(h *TrackHandler) { h.now = now }
}

// Routes registers the track endpoints. Only the create is throttled per user:
// it is the one route here that grows the account's rows, and the reads and
// single-row edits behind it cost nothing an account can accumulate.
func (h *TrackHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.handleListTracks)
	r.With(h.writeLimiter.middleware).Post("/", h.handleCreateTrack)
	r.Get("/{trackId}/status", h.handleGetTrackStatus)
	r.Patch("/{trackId}/track-number", h.handleSetTrackNumber)
	r.Delete("/{trackId}", h.handleDeleteTrack)
	h.featuredArtist.Routes(r)
	return r
}

// handleSetTrackNumber fills a track's album position once. It answers 204
// whether or not the write-once update applied to an owned track: the client
// sends the number best-effort on album-context saves, so an already-set number
// is not an error. The no-op is logged as track.track_number_unchanged so it
// stays observable. A missing or foreign track answers 404 (ErrTrackNotFound).
func (h *TrackHandler) handleSetTrackNumber(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	trackId, ok := httputil.PathID(w, r, "trackId", domain.ParseTrackId, "invalid track ID")
	if !ok {
		return
	}
	var req SetTrackNumberRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}
	updated, err := h.setTrackNumber.Execute(r.Context(), userId, trackId, req.TrackNumber)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}
	event := "track.track_number_set"
	if !updated {
		event = "track.track_number_unchanged"
	}
	slog.InfoContext(r.Context(), event,
		"track_id", trackId.String(),
		"track_number", req.TrackNumber,
	)
	w.WriteHeader(http.StatusNoContent)
}

type SetTrackNumberRequest struct {
	TrackNumber int `json:"track_number"`
}

type CreateTrackRequest struct {
	Title           string                      `json:"title"`
	Artist          string                      `json:"artist"`
	Album           *string                     `json:"album,omitempty"`
	DurationSeconds *float64                    `json:"duration_seconds,omitempty"`
	ArtworkURL      *string                     `json:"artwork_url,omitempty"`
	ISRC            *string                     `json:"isrc,omitempty"`
	Year            *int                        `json:"year,omitempty"`
	Genre           *string                     `json:"genre,omitempty"`
	AlbumArtist     *string                     `json:"album_artist,omitempty"`
	TrackNumber     *int                        `json:"track_number,omitempty"`
	FeaturedArtists []service.FeaturedArtistDTO `json:"featured_artists,omitempty"`
	SourceURL       *string                     `json:"source_url,omitempty"`
}

type TrackResponse = service.TrackDTO

type ListTracksResponse struct {
	Items   []TrackResponse `json:"items"`
	Total   int             `json:"total"`
	Limit   int             `json:"limit"`
	Offset  int             `json:"offset"`
	HasMore bool            `json:"has_more"`
}

func tracksToDTO(tracks []*domain.Track) []TrackResponse {
	out := make([]TrackResponse, len(tracks))
	for i, t := range tracks {
		out[i] = service.TrackToDTO(t)
	}
	return out
}

func (h *TrackHandler) handleListTracks(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	query, err := libraryQuery(r)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	result, err := h.listTracks.Execute(r.Context(), userId, query)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, ListTracksResponse{
		Items:   tracksToDTO(result.Tracks),
		Total:   result.Total,
		Limit:   result.Limit,
		Offset:  query.Offset,
		HasMore: result.HasMore,
	})
}

func (h *TrackHandler) handleCreateTrack(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	var req CreateTrackRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	album := ""
	if req.Album != nil {
		album = *req.Album
	}

	input := service.AddTrackInput{
		IdempotencyKey:  idempotencyKey(r),
		Title:           req.Title,
		Artist:          req.Artist,
		Album:           album,
		DurationSeconds: req.DurationSeconds,
		ArtworkURL:      req.ArtworkURL,
		Year:            req.Year,
		Genre:           req.Genre,
		ISRC:            req.ISRC,
		AlbumArtist:     req.AlbumArtist,
		TrackNumber:     req.TrackNumber,
		FeaturedArtists: domainFeaturedFromDTOs(req.FeaturedArtists),
		SourceURL:       req.SourceURL,
	}

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

// idempotencyKey reads the optional client-supplied Idempotency-Key header. An
// absent or blank header yields nil, meaning the create falls back to
// content-based dedup; a present key collapses concurrent creates and retries of
// the same logical save onto a single row.
func idempotencyKey(r *http.Request) *string {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		return nil
	}
	return &key
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

	trackId, ok := httputil.PathID(w, r, "trackId", domain.ParseTrackId, "invalid track ID")
	if !ok {
		return
	}

	track, err := h.getTrackStatus.Execute(r.Context(), userId, trackId)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
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

	trackId, ok := httputil.PathID(w, r, "trackId", domain.ParseTrackId, "invalid track ID")
	if !ok {
		return
	}

	slog.InfoContext(r.Context(), "track.delete",
		"track_id", trackId.String(), "user_id", userId.String())

	err := h.deleteTrack.Execute(r.Context(), userId, trackId)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func domainFeaturedFromDTOs(dtos []service.FeaturedArtistDTO) []domain.FeaturedArtist {
	if len(dtos) == 0 {
		return nil
	}
	out := make([]domain.FeaturedArtist, 0, len(dtos))
	for _, d := range dtos {
		mbid := ""
		if d.MBID != nil {
			mbid = *d.MBID
		}
		deezerID := int64(0)
		if d.DeezerID != nil {
			deezerID = *d.DeezerID
		}
		if fa, ok := domain.NewFeaturedArtist(d.Name, mbid, deezerID); ok {
			out = append(out, fa)
		}
	}
	return out
}
