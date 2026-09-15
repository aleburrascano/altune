package handler

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// trackAdmission gates a command on the track's state and cooldown, running
// schedule only when admitted and keeping the cooldown only if it succeeds.
type trackAdmission interface {
	Admit(track *domain.Track, schedule func() error) error
}

type acquisitionCommand struct {
	trackRepo ports.TrackRepository
	admission trackAdmission
	logMsg    string
	// schedule queues the job; a non-nil error means nothing was queued.
	schedule func(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error
}

func (c acquisitionCommand) serve(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	trackId, err := domain.ParseTrackId(chi.URLParam(r, "trackId"))
	if err != nil {
		httputil.BadRequest(w, "invalid track ID")
		return
	}

	track, err := c.trackRepo.GetByID(r.Context(), trackId, userId)
	if err != nil {
		slog.ErrorContext(r.Context(), c.logMsg, "error", err, "track_id", trackId.String())
		httputil.InternalError(w)
		return
	}
	if track == nil {
		httputil.NotFound(w, "track not found")
		return
	}

	schedule := func() error { return c.schedule(r.Context(), userId, trackId) }
	if err := c.admission.Admit(track, schedule); err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}
