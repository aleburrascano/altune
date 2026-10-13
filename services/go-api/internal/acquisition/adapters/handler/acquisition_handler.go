package handler

import (
	"altune/go-api/internal/acquisition/adapters/authbridge"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type trackAdmission interface {
	Admit(ctx context.Context, track *ports.TrackSnapshot, schedule func() error) error
}

type acquisitionCommand struct {
	trackRepo ports.TrackLedger
	admission trackAdmission
	logMsg    string
	schedule  func(ctx context.Context, userId shared.UserId, trackId shared.TrackId) error
}

func (c acquisitionCommand) serve(w http.ResponseWriter, r *http.Request) {
	userId, ok := authbridge.RequireUserID(w, r)
	if !ok {
		return
	}

	trackId, track, ok := c.resolveTrack(w, r, userId)
	if !ok {
		return
	}

	schedule := func() error { return c.schedule(r.Context(), userId, trackId) }
	if err := c.admission.Admit(r.Context(), track, schedule); err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (c acquisitionCommand) resolveTrack(w http.ResponseWriter, r *http.Request, userId shared.UserId) (shared.TrackId, *ports.TrackSnapshot, bool) {
	trackId, err := shared.ParseTrackId(chi.URLParam(r, "trackId"))
	if err != nil {
		httputil.BadRequest(w, "invalid track ID")
		return trackId, nil, false
	}
	track, ok := c.loadTrack(w, r, userId, trackId)
	return trackId, track, ok
}

func (c acquisitionCommand) loadTrack(w http.ResponseWriter, r *http.Request, userId shared.UserId, trackId shared.TrackId) (*ports.TrackSnapshot, bool) {
	track, err := c.trackRepo.Get(r.Context(), trackId, userId)
	if err != nil {
		slog.ErrorContext(r.Context(), c.logMsg, "error", err, "track_id", trackId.String())
		httputil.InternalError(w)
		return nil, false
	}
	if track == nil {
		httputil.NotFound(w, "track not found")
		return nil, false
	}
	return track, true
}

type replaceScheduler interface {
	ScheduleReplace(ctx context.Context, userId shared.UserId, trackId shared.TrackId) error
}

type ReacquireHandler struct {
	trackRepo ports.TrackLedger
	scheduler replaceScheduler
	admission trackAdmission
}

func NewReacquireHandler(trackRepo ports.TrackLedger, scheduler replaceScheduler, admission trackAdmission) *ReacquireHandler {
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
		schedule: func(ctx context.Context, userId shared.UserId, trackId shared.TrackId) error {
			return h.scheduler.ScheduleReplace(ctx, userId, trackId)
		},
	}.serve(w, r)
}

type acquisitionScheduler interface {
	Schedule(ctx context.Context, userId shared.UserId, trackId shared.TrackId, sourceURL string) error
}

type RetryHandler struct {
	trackRepo ports.TrackLedger
	scheduler acquisitionScheduler
	admission trackAdmission
}

func NewRetryHandler(
	trackRepo ports.TrackLedger,
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
		schedule: func(ctx context.Context, userId shared.UserId, trackId shared.TrackId) error {
			return h.scheduler.Schedule(ctx, userId, trackId, "")
		},
	}.serve(w, r)
}
