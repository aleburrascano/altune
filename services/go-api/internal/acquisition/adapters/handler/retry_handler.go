package handler

import (
	"net/http"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"

	"github.com/go-chi/chi/v5"
)

type acquisitionScheduler interface {
	Schedule(userId shared.UserId, trackId domain.TrackId)
}

type RetryHandler struct {
	trackRepo ports.TrackRepository
	scheduler acquisitionScheduler
}

func NewRetryHandler(
	trackRepo ports.TrackRepository,
	scheduler acquisitionScheduler,
) *RetryHandler {
	return &RetryHandler{
		trackRepo: trackRepo,
		scheduler: scheduler,
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

	h.scheduler.Schedule(userId, trackId)

	w.WriteHeader(http.StatusAccepted)
}
