package handler

import (
	"altune/go-api/internal/library/internal/domain"
	trackservice "altune/go-api/internal/library/internal/service/track"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/requestuser"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type TrackHandler struct {
	addTrack       *trackservice.AddTrackService
	listTracks     *trackservice.ListTracksService
	getTrackStatus *trackservice.GetTrackStatusService
	deleteTrack    *trackservice.DeleteTrackService
	setTrackNumber *trackservice.SetTrackNumberService
	featuredArtist *FeaturedArtistHandler
	now            func() time.Time
	writeLimiter   rateLimitMiddleware
}

func NewTrackHandler(
	addTrack *trackservice.AddTrackService,
	listTracks *trackservice.ListTracksService,
	getTrackStatus *trackservice.GetTrackStatusService,
	deleteTrack *trackservice.DeleteTrackService,
	setTrackNumber *trackservice.SetTrackNumberService,
	featuredArtist *FeaturedArtistHandler,
	now clock.Func,
	opts ...func(*TrackHandler),
) (*TrackHandler, error) {
	if now == nil {
		return nil, fmt.Errorf("NewTrackHandler: %w", ErrClockRequired)
	}
	h := &TrackHandler{
		addTrack:       addTrack,
		listTracks:     listTracks,
		getTrackStatus: getTrackStatus,
		deleteTrack:    deleteTrack,
		setTrackNumber: setTrackNumber,
		featuredArtist: featuredArtist,
		now:            now,
	}
	for _, opt := range opts {
		opt(h)
	}
	h.writeLimiter = newWriteRateLimiter(trackWriteRateLimit, h.now)
	return h, nil
}

func MustNewTrackHandler(
	addTrack *trackservice.AddTrackService,
	listTracks *trackservice.ListTracksService,
	getTrackStatus *trackservice.GetTrackStatusService,
	deleteTrack *trackservice.DeleteTrackService,
	setTrackNumber *trackservice.SetTrackNumberService,
	featuredArtist *FeaturedArtistHandler,
	now clock.Func,
	opts ...func(*TrackHandler),
) *TrackHandler {
	h, err := NewTrackHandler(addTrack, listTracks, getTrackStatus, deleteTrack, setTrackNumber, featuredArtist, now, opts...)
	if err != nil {
		panic(err)
	}
	return h
}

func (h *TrackHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.handleListTracks)
	r.With(h.writeLimiter).Post("/", h.handleCreateTrack)
	r.Get("/{trackId}/status", h.handleGetTrackStatus)
	r.Patch("/{trackId}/track-number", h.handleSetTrackNumber)
	r.Delete("/{trackId}", h.handleDeleteTrack)
	h.featuredArtist.Routes(r)
	return r
}

func (h *TrackHandler) handleSetTrackNumber(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)
	trackId, ok := httpserver.PathID(w, r, "trackId", domain.ParseTrackId, "invalid track ID")
	if !ok {
		return
	}
	var req SetTrackNumberRequest
	if !httpserver.DecodeJSON(w, r, &req) {
		return
	}
	updated, err := h.setTrackNumber.Execute(r.Context(), userId, trackId, req.TrackNumber)
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}
	event := "track.track_number_set"
	if !updated {
		event = "track.track_number_unchanged"
	}
	slog.InfoContext(r.Context(), event,
		logging.KeyTrackID, trackId.String(),
		"track_number", req.TrackNumber,
	)
	w.WriteHeader(http.StatusNoContent)
}

type SetTrackNumberRequest struct {
	TrackNumber int `json:"track_number"`
}

type CreateTrackRequest struct {
	Title           string                           `json:"title"`
	Artist          string                           `json:"artist"`
	Album           *string                          `json:"album,omitempty"`
	DurationSeconds *float64                         `json:"duration_seconds,omitempty"`
	ArtworkURL      *string                          `json:"artwork_url,omitempty"`
	ISRC            *string                          `json:"isrc,omitempty"`
	Year            *int                             `json:"year,omitempty"`
	Genre           *string                          `json:"genre,omitempty"`
	AlbumArtist     *string                          `json:"album_artist,omitempty"`
	TrackNumber     *int                             `json:"track_number,omitempty"`
	FeaturedArtists []trackservice.FeaturedArtistDTO `json:"featured_artists,omitempty"`
	SourceURL       *string                          `json:"source_url,omitempty"`
	MBID            *string                          `json:"mbid,omitempty"`
	Explicit        *bool                            `json:"explicit,omitempty"`
	Sources         []CatalogSourceRequest           `json:"sources,omitempty"`
}

type CatalogSourceRequest struct {
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id"`
}

func catalogSourcesFromRequest(sources []CatalogSourceRequest) []domain.CatalogSource {
	out := make([]domain.CatalogSource, len(sources))
	for i, s := range sources {
		out[i] = domain.CatalogSource{Provider: s.Provider, ExternalID: s.ExternalID}
	}
	return out
}

type TrackResponse = trackservice.TrackDTO

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
		out[i] = trackservice.TrackToDTO(t)
	}
	return out
}

func (h *TrackHandler) handleListTracks(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	query, err := libraryQuery(r)
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}

	result, err := h.listTracks.Execute(r.Context(), userId, query)
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, ListTracksResponse{
		Items:   tracksToDTO(result.Tracks),
		Total:   result.Total,
		Limit:   result.Limit,
		Offset:  query.Offset,
		HasMore: result.HasMore,
	})
}

func (h *TrackHandler) handleCreateTrack(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	var req CreateTrackRequest
	if !httpserver.DecodeJSON(w, r, &req) {
		return
	}

	album := ""
	if req.Album != nil {
		album = *req.Album
	}

	input := trackservice.AddTrackInput{
		IdempotencyKey:  httpserver.IdempotencyKey(r),
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
		CatalogMBID:     req.MBID,
		CatalogExplicit: req.Explicit,
		CatalogSources:  catalogSourcesFromRequest(req.Sources),
	}

	result, err := h.addTrack.Execute(r.Context(), userId, input)
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}

	status := http.StatusOK
	switch {
	case result.Created:
		status = http.StatusCreated
		slog.InfoContext(r.Context(), "track.saved",
			logging.KeyTrackID, result.Track.ID.String(),
			"title", result.Track.Title,
			"artist", result.Track.Artist,
			"album", result.Track.Album,
		)
	default:
		slog.InfoContext(r.Context(), "track.dedup_hit",
			logging.KeyTrackID, result.Track.ID.String(),
			"title", result.Track.Title,
			logging.KeyStatus, result.Track.DownloadStatus.String(),
		)
	}

	httpserver.WriteJSON(w, status, trackservice.TrackToDTO(result.Track))
}

type TrackStatusResponse struct {
	ID             uuid.UUID `json:"id"`
	DownloadStatus string    `json:"acquisition_status"`
	FailureReason  *string   `json:"failure_reason,omitempty"`
}

func (h *TrackHandler) handleGetTrackStatus(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	trackId, ok := httpserver.PathID(w, r, "trackId", domain.ParseTrackId, "invalid track ID")
	if !ok {
		return
	}

	track, err := h.getTrackStatus.Execute(r.Context(), userId, trackId)
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, TrackStatusResponse{
		ID:             track.ID.UUID(),
		DownloadStatus: track.DownloadStatus.String(),
		FailureReason:  track.FailureReason,
	})
}

func (h *TrackHandler) handleDeleteTrack(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	trackId, ok := httpserver.PathID(w, r, "trackId", domain.ParseTrackId, "invalid track ID")
	if !ok {
		return
	}

	err := h.deleteTrack.Execute(r.Context(), userId, trackId)
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func domainFeaturedFromDTOs(dtos []trackservice.FeaturedArtistDTO) []domain.FeaturedArtist {
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
