package handler

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"net/http"
)

type replaceScheduler interface {
	ScheduleReplace(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error
}

type ReacquireHandler struct {
	trackRepo ports.TrackRepository
	scheduler replaceScheduler
	admission trackAdmission
}

func NewReacquireHandler(trackRepo ports.TrackRepository, scheduler replaceScheduler, admission trackAdmission) *ReacquireHandler {
	return &ReacquireHandler{
		trackRepo: trackRepo,
		scheduler: scheduler,
		admission: admission,
	}
}

func (h *ReacquireHandler) HandleReacquire(w http.ResponseWriter, r *http.Request) {
	acquisitionCommand{
		trackRepo: h.trackRepo,
		admission: h.admission,
		logMsg:    "reacquire: get track failed",
		schedule: func(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
			return h.scheduler.ScheduleReplace(ctx, userId, trackId)
		},
	}.serve(w, r)
}
