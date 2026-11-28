package handler

import (
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/requestuser"
	"context"
	"errors"
	"fmt"
	"net/http"
)

type trackAdmission interface {
	Admit(ctx context.Context, track *ports.TrackSnapshot, schedule func() error) error
}

type acquisitionCommand struct {
	trackRepo ports.LibraryTracks
	admission trackAdmission
	schedule  func(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error
}

func (c acquisitionCommand) serve(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	trackId, track, ok := c.resolveTrack(w, r, userId)
	if !ok {
		return
	}

	ctx := r.Context()
	schedule := func() error { return c.schedule(ctx, userId, trackId) }
	if err := c.admission.Admit(ctx, track, schedule); err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (c acquisitionCommand) resolveTrack(w http.ResponseWriter, r *http.Request, userId ids.UserId) (ids.TrackId, *ports.TrackSnapshot, bool) {
	trackId, err := ids.ParseTrackId(httpserver.URLParam(r, "trackId"))
	if err != nil {
		httpserver.HandleServiceError(w, r, apierror.NewError(apierror.CodeCatalogInvalidTrackID, "invalid track ID"))
		return trackId, nil, false
	}
	track, ok := c.loadTrack(w, r, userId, trackId)
	return trackId, track, ok
}

func (c acquisitionCommand) loadTrack(w http.ResponseWriter, r *http.Request, userId ids.UserId, trackId ids.TrackId) (*ports.TrackSnapshot, bool) {
	track, err := c.trackRepo.Get(r.Context(), trackId, userId)
	if errors.Is(err, ports.ErrTrackNotFound) {
		httpserver.HandleServiceError(w, r, apierror.NewError(apierror.CodeCatalogTrackNotFound, "track not found"))
		return nil, false
	}
	if err != nil {
		httpserver.HandleServiceError(w, r, fmt.Errorf("get track %s: %w", trackId, err))
		return nil, false
	}
	return track, true
}

type replaceScheduler interface {
	ScheduleReplace(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error
}

type ReacquireHandler struct {
	trackRepo ports.LibraryTracks
	scheduler replaceScheduler
	admission trackAdmission
}

func NewReacquireHandler(trackRepo ports.LibraryTracks, scheduler replaceScheduler, admission trackAdmission) *ReacquireHandler {
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
		schedule: func(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error {
			return h.scheduler.ScheduleReplace(ctx, userId, trackId)
		},
	}.serve(w, r)
}

type acquisitionScheduler interface {
	Schedule(ctx context.Context, userId ids.UserId, trackId ids.TrackId, sourceURL string) error
}

type RetryHandler struct {
	trackRepo ports.LibraryTracks
	scheduler acquisitionScheduler
	admission trackAdmission
}

func NewRetryHandler(
	trackRepo ports.LibraryTracks,
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
		schedule: func(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error {
			return h.scheduler.Schedule(ctx, userId, trackId, "")
		},
	}.serve(w, r)
}
