package handler

import (
	"net/http"
	"sync"
	"time"

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

const retryCooldown = 60 * time.Second

type RetryHandler struct {
	trackRepo ports.TrackRepository
	scheduler acquisitionScheduler
	mu        sync.Mutex
	lastRetry map[string]time.Time
}

func NewRetryHandler(
	trackRepo ports.TrackRepository,
	scheduler acquisitionScheduler,
) *RetryHandler {
	return &RetryHandler{
		trackRepo: trackRepo,
		scheduler: scheduler,
		lastRetry: make(map[string]time.Time),
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

	key := trackId.String()
	h.mu.Lock()
	now := time.Now()
	if last, ok := h.lastRetry[key]; ok && now.Sub(last) < retryCooldown {
		h.mu.Unlock()
		httputil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": "retry cooldown active, try again later",
		})
		return
	}
	h.lastRetry[key] = now
	for k, v := range h.lastRetry {
		if now.Sub(v) >= 2*retryCooldown {
			delete(h.lastRetry, k)
		}
	}
	h.mu.Unlock()

	h.scheduler.Schedule(userId, trackId)

	w.WriteHeader(http.StatusAccepted)
}
