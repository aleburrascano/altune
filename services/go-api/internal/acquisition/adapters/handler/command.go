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

type trackAdmission interface {
	Admit(track *domain.Track) error
}

type acquisitionCommand struct {
	trackRepo     ports.TrackRepository
	admission     trackAdmission
	ineligibleErr error
	ineligibleMsg string
	cooldownMsg   string
	logMsg        string
	schedule      func(userId shared.UserId, trackId domain.TrackId)
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

	switch err := c.admission.Admit(track); {
	case errors.Is(err, c.ineligibleErr):
		httputil.Conflict(w, c.ineligibleMsg)
		return
	case errors.Is(err, service.ErrCooldownActive):
		httputil.WriteJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": c.cooldownMsg,
		})
		return
	}

	c.schedule(userId, trackId)

	w.WriteHeader(http.StatusAccepted)
}
