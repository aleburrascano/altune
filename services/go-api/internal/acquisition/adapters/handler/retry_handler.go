package handler

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"net/http"
)

type acquisitionScheduler interface {
	Schedule(ctx context.Context, userId shared.UserId, trackId domain.TrackId, sourceURL string) error
}

type RetryHandler struct {
	trackRepo ports.TrackRepository
	scheduler acquisitionScheduler
	admission trackAdmission
}

func NewRetryHandler(
	trackRepo ports.TrackRepository,
	scheduler acquisitionScheduler,
	admission trackAdmission,
) *RetryHandler {
	return &RetryHandler{
		trackRepo: trackRepo,
		scheduler: scheduler,
		admission: admission,
	}
}

func (h *RetryHandler) HandleRetryAcquisition(w http.ResponseWriter, r *http.Request) {
	acquisitionCommand{
		trackRepo: h.trackRepo,
		admission: h.admission,
		logMsg:    "retry acquisition: get track failed",
		schedule: func(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
			return h.scheduler.Schedule(ctx, userId, trackId, "")
		},
	}.serve(w, r)
}
