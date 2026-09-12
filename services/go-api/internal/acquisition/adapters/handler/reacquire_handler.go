package handler

import (
	"net/http"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

type replaceScheduler interface {
	ScheduleReplace(userId shared.UserId, trackId domain.TrackId)
}

type ReacquireHandler struct {
	trackRepo ports.TrackRepository
	scheduler replaceScheduler
	admission *service.ReacquireAdmission
}

func NewReacquireHandler(trackRepo ports.TrackRepository, scheduler replaceScheduler, admission *service.ReacquireAdmission) *ReacquireHandler {
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
		schedule: func(userId shared.UserId, trackId domain.TrackId) {
			h.scheduler.ScheduleReplace(userId, trackId)
		},
	}.serve(w, r)
}
