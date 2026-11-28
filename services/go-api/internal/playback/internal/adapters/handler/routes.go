package handler

import (
	"altune/go-api/internal/playback/internal/ports"
	"altune/go-api/internal/playback/internal/service"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/requestuser"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type QueueHandler struct {
	svc     *service.QueueService
	limiter func(http.Handler) http.Handler
}

var (
	ErrCollaboratorRequired = errors.New("handler: a required collaborator is missing")
	ErrClockRequired        = errors.New("handler: the queue handler requires a clock")
)

func NewQueueHandler(svc *service.QueueService, limit QueueSaveLimit, now func() time.Time, metrics ports.RateLimitMetrics) (*QueueHandler, error) {
	switch {
	case svc == nil:
		return nil, fmt.Errorf("NewQueueHandler: the queue service: %w", ErrCollaboratorRequired)
	case now == nil:
		return nil, fmt.Errorf("NewQueueHandler: %w", ErrClockRequired)
	case metrics == nil:
		return nil, fmt.Errorf("NewQueueHandler: the rate limit metrics: %w", ErrCollaboratorRequired)
	}
	return &QueueHandler{svc: svc, limiter: newUserRateLimiter(limit, now, metrics)}, nil
}

func MustNewQueueHandler(svc *service.QueueService, limit QueueSaveLimit, now func() time.Time, metrics ports.RateLimitMetrics) *QueueHandler {
	h, err := NewQueueHandler(svc, limit, now, metrics)
	if err != nil {
		panic(err)
	}
	return h
}

type Routes struct {
	queue *QueueHandler
}

func NewRoutes(queue *QueueHandler) Routes {
	return Routes{queue: queue}
}

func (rt Routes) Bounded(r chi.Router) {
	r.Mount("/playback", rt.queue.Routes())
}

func (rt Routes) LongLived(chi.Router) {}

func (h *QueueHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.With(h.limiter).Put("/queue-state", h.handleSave)
	r.With(h.limiter).Put("/queue-state/position", h.handleSavePosition)
	r.With(h.limiter).Get("/queue-state", h.handleGet)
	r.Delete("/queue-state", h.handleForget)
	return r
}

func (h *QueueHandler) handleSave(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	var body saveQueueRequest
	if !httpserver.DecodeJSON(w, r, &body) {
		return
	}

	err := h.svc.Save(r.Context(), userId, service.SaveQueueStateInput{
		TrackIds:       body.TrackIds,
		CurrentIdx:     body.CurrentIdx,
		PositionMs:     body.PositionMs,
		Shuffled:       body.Shuffled,
		RepeatMode:     body.RepeatMode,
		Source:         sourceFromDTO(body.Source),
		LegacySourceId: body.SourceId,
		NaturalOrder:   body.NaturalOrder,
	})
	if err != nil {
		writeQueueError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *QueueHandler) handleSavePosition(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	var body saveQueuePositionRequest
	if !httpserver.DecodeJSON(w, r, &body) {
		return
	}

	err := h.svc.SavePosition(r.Context(), userId, service.SaveQueuePositionInput{
		CurrentIdx:     body.CurrentIdx,
		CurrentTrackId: body.CurrentTrackId,
		PositionMs:     body.PositionMs,
	})
	if err != nil {
		writeQueueError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *QueueHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	view, err := h.svc.ResumeView(r.Context(), userId)
	if err != nil {
		writeQueueError(w, r, err)
		return
	}

	w.Header().Set("Cache-Control", httpserver.CacheControlPrivateNoStore)
	httpserver.WriteJSON(w, http.StatusOK, toResponse(view))
}

func (h *QueueHandler) handleForget(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	if err := h.svc.Forget(r.Context(), userId); err != nil {
		writeQueueError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
