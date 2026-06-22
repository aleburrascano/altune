package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/service"
	"altune/go-api/internal/shared/httputil"
)

type QueueHandler struct {
	svc *service.QueueService
}

func NewQueueHandler(svc *service.QueueService) *QueueHandler {
	return &QueueHandler{svc: svc}
}

func (h *QueueHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Put("/queue-state", h.handleSave)
	r.Get("/queue-state", h.handleGet)
	return r
}

type saveQueueRequest struct {
	TrackIds   []string `json:"track_ids"`
	CurrentIdx int      `json:"current_index"`
	PositionMs int64    `json:"position_ms"`
	Shuffled   bool     `json:"shuffled"`
	RepeatMode string   `json:"repeat_mode"`
	SourceId   string   `json:"source_id"`
}

type queueStateResponse struct {
	TrackIds   []string `json:"track_ids"`
	CurrentIdx int      `json:"current_index"`
	PositionMs int64    `json:"position_ms"`
	Shuffled   bool     `json:"shuffled"`
	RepeatMode string   `json:"repeat_mode"`
	SourceId   string   `json:"source_id"`
}

func (h *QueueHandler) handleSave(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	var body saveQueueRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := h.svc.Save(r.Context(), userId, service.SaveQueueStateInput{
		TrackIds:   body.TrackIds,
		CurrentIdx: body.CurrentIdx,
		PositionMs: body.PositionMs,
		Shuffled:   body.Shuffled,
		RepeatMode: body.RepeatMode,
		SourceId:   body.SourceId,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "playback.queue_state.save_failed", "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to save queue state")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *QueueHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	state, err := h.svc.Resume(r.Context(), userId)
	if err != nil {
		slog.ErrorContext(r.Context(), "playback.queue_state.resume_failed", "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to get queue state")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, toResponse(state))
}

func toResponse(state *domain.QueueState) queueStateResponse {
	// QueueState guarantees a non-nil TrackIds (EmptyQueueState / RehydrateQueueState
	// both initialise it), so no nil-to-empty normalization is needed here.
	return queueStateResponse{
		TrackIds:   state.TrackIds,
		CurrentIdx: state.CurrentIdx,
		PositionMs: state.PositionMs,
		Shuffled:   state.Shuffled,
		RepeatMode: state.RepeatMode.String(),
		SourceId:   state.SourceId,
	}
}
