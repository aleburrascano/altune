package handler

import (
	"errors"
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

type replaceScheduler interface {
	ScheduleReplace(userId shared.UserId, trackId domain.TrackId)
}

type ReacquireHandler struct {
	trackRepo ports.TrackRepository
	scheduler replaceScheduler
	admission *service.ReacquireAdmission
}

func NewReacquireHandler(trackRepo ports.TrackRepository, scheduler replaceScheduler) *ReacquireHandler {
	return &ReacquireHandler{
		trackRepo: trackRepo,
		scheduler: scheduler,
		admission: service.NewReacquireAdmission(),
	}
}

func (h *ReacquireHandler) HandleReacquire(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	trackId, err := domain.ParseTrackId(chi.URLParam(r, "trackId"))
	if err != nil {
		httputil.BadRequest(w, "invalid track ID")
		return
	}

	track, err := h.trackRepo.GetByID(r.Context(), trackId, userId)
	if err != nil {
		slog.ErrorContext(r.Context(), "reacquire: get track failed",
			"error", err, "track_id", trackId.String())
		httputil.InternalError(w)
		return
	}
	if track == nil {
		httputil.NotFound(w, "track not found")
		return
	}

	switch err := h.admission.Admit(track); {
	case errors.Is(err, service.ErrReacquireNotReady):
		httputil.Conflict(w, "track has no audio to replace")
		return
	case errors.Is(err, service.ErrRetryCooldown):
		httputil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": "reacquire cooldown active, try again later",
		})
		return
	}

	h.scheduler.ScheduleReplace(userId, trackId)

	w.WriteHeader(http.StatusAccepted)
}
