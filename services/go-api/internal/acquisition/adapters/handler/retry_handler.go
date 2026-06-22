package handler

import (
	"log/slog"
	"net/http"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"

	"github.com/go-chi/chi/v5"
)

type acquisitionScheduler interface {
	Schedule(userId shared.UserId, trackId domain.TrackId, sourceURL string)
}

type RetryHandler struct {
	trackRepo ports.TrackRepository
	scheduler acquisitionScheduler
	admission *service.RetryAdmission
}

func NewRetryHandler(
	trackRepo ports.TrackRepository,
	scheduler acquisitionScheduler,
) *RetryHandler {
	return &RetryHandler{
		trackRepo: trackRepo,
		scheduler: scheduler,
		admission: service.NewRetryAdmission(),
	}
}

func (h *RetryHandler) HandleRetryAcquisition(w http.ResponseWriter, r *http.Request) {
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

	track, err := h.trackRepo.GetByID(r.Context(), trackId, userId)
	if err != nil {
		slog.ErrorContext(r.Context(), "retry acquisition: get track failed",
			"error", err, "track_id", trackIdStr)
		httputil.InternalError(w)
		return
	}
	if track == nil {
		httputil.NotFound(w, "track not found")
		return
	}

	if track.AcquisitionStatus != domain.AcquisitionFailed {
		httputil.Conflict(w, "track is not in failed state")
		return
	}

	if !h.admission.Allow(trackId) {
		httputil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": "retry cooldown active, try again later",
		})
		return
	}

	// Retries carry no source URL (the request is by trackId), so acquisition
	// falls back to the search pipeline.
	h.scheduler.Schedule(userId, trackId, "")

	w.WriteHeader(http.StatusAccepted)
}
