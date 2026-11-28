package handler

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/library/internal/service"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/requestuser"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type TrackHandler struct {
	addTrack       *service.AddTrackService
	listTracks     *service.ListTracksService
	getTrackStatus *service.GetTrackStatusService
	deleteTrack    *service.DeleteTrackService
	setTrackNumber *service.SetTrackNumberService
	featuredArtist *FeaturedArtistHandler
	now            func() time.Time
	writeLimiter   rateLimitMiddleware
}

func NewTrackHandler(
	addTrack *service.AddTrackService,
	listTracks *service.ListTracksService,
	getTrackStatus *service.GetTrackStatusService,
	deleteTrack *service.DeleteTrackService,
	setTrackNumber *service.SetTrackNumberService,
	featuredArtist *FeaturedArtistHandler,
	now clock.Func,
	opts ...func(*TrackHandler),
) (*TrackHandler, error) {
	if now == nil {
		return nil, fmt.Errorf("NewTrackHandler: %w", domain.ErrHandlerClockRequired)
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
	addTrack *service.AddTrackService,
	listTracks *service.ListTracksService,
	getTrackStatus *service.GetTrackStatusService,
	deleteTrack *service.DeleteTrackService,
	setTrackNumber *service.SetTrackNumberService,
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
		HandleError(w, r, err)
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

func catalogSourcesFromRequest(sources []CatalogSourceRequest) []domain.CatalogSource {
	out := make([]domain.CatalogSource, len(sources))
	for i, s := range sources {
		out[i] = domain.CatalogSource{Provider: s.Provider, ExternalID: s.ExternalID}
	}
	return out
}

func (h *TrackHandler) handleListTracks(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	query, err := libraryQuery(r)
	if err != nil {
		HandleError(w, r, err)
		return
	}

	result, err := h.listTracks.Execute(r.Context(), userId, query)
	if err != nil {
		HandleError(w, r, err)
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

	input := service.AddTrackInput{
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
		HandleError(w, r, err)
		return
	}

	status := http.StatusOK
	switch {
	case result.Created:
		status = http.StatusCreated
		slog.InfoContext(r.Context(), "track.saved",
			logging.KeyTrackID, result.Track.ID().String(),
			"title", result.Track.Title(),
			"artist", result.Track.Artist(),
			"album", result.Track.Album(),
		)
	default:
		slog.InfoContext(r.Context(), "track.dedup_hit",
			logging.KeyTrackID, result.Track.ID().String(),
			"title", result.Track.Title(),
			logging.KeyStatus, result.Track.DownloadStatus().String(),
		)
	}

	httpserver.WriteJSON(w, status, TrackToDTO(result.Track))
}

func (h *TrackHandler) handleGetTrackStatus(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	trackId, ok := httpserver.PathID(w, r, "trackId", domain.ParseTrackId, "invalid track ID")
	if !ok {
		return
	}

	track, err := h.getTrackStatus.Execute(r.Context(), userId, trackId)
	if err != nil {
		HandleError(w, r, err)
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, TrackStatusResponse{
		ID:             track.ID().UUID(),
		DownloadStatus: track.DownloadStatus().String(),
		FailureReason:  track.FailureReason(),
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
		HandleError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
