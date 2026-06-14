package handler

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	acqservice "altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/shared/httputil"

	"github.com/go-chi/chi/v5"
)

type RetryHandler struct {
	trackRepo  ports.TrackRepository
	acquireSvc *acqservice.AcquireTrackAudioService
	wg         *sync.WaitGroup
	sem        chan struct{}
}

func NewRetryHandler(
	trackRepo ports.TrackRepository,
	acquireSvc *acqservice.AcquireTrackAudioService,
	wg *sync.WaitGroup,
	sem chan struct{},
) *RetryHandler {
	return &RetryHandler{
		trackRepo:  trackRepo,
		acquireSvc: acquireSvc,
		wg:         wg,
		sem:        sem,
	}
}

func (h *RetryHandler) HandleRetryAcquisition(w http.ResponseWriter, r *http.Request) {
	userId := auth.MustUserID(r.Context())
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

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()

		h.sem <- struct{}{}
		defer func() { <-h.sem }()

		bgCtx := context.Background()
		if err := h.acquireSvc.Execute(bgCtx, userId, trackId); err != nil {
			slog.Error("retry acquisition failed",
				"track_id", trackId.String(), "error", err)
		}
	}()

	w.WriteHeader(http.StatusAccepted)
}
