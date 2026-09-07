package handler

import (
	"net/http"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
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
	admission *service.RetryAdmission,
) *RetryHandler {
	return &RetryHandler{
		trackRepo: trackRepo,
		scheduler: scheduler,
		admission: admission,
	}
}

func (h *RetryHandler) HandleRetryAcquisition(w http.ResponseWriter, r *http.Request) {
	acquisitionCommand{
		trackRepo:     h.trackRepo,
		admission:     h.admission,
		ineligibleErr: service.ErrRetryNotFailed,
		ineligibleMsg: "track is not in failed state",
		cooldownMsg:   "retry cooldown active, try again later",
		logMsg:        "retry acquisition: get track failed",
		schedule: func(userId shared.UserId, trackId domain.TrackId) {
			h.scheduler.Schedule(userId, trackId, "")
		},
	}.serve(w, r)
}
