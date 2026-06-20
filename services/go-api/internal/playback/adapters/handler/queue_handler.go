package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/playback/service"
	"altune/go-api/internal/shared/httputil"
)

type QueueHandler struct {
	save *service.SaveQueueStateService
	get  *service.GetQueueStateService
}

func NewQueueHandler(
	save *service.SaveQueueStateService,
	get *service.GetQueueStateService,
) *QueueHandler {
	return &QueueHandler{save: save, get: get}
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

	err := h.save.Execute(r.Context(), userId, service.SaveQueueStateInput{
		TrackIds:   body.TrackIds,
		CurrentIdx: body.CurrentIdx,
		PositionMs: body.PositionMs,
		Shuffled:   body.Shuffled,
		RepeatMode: body.RepeatMode,
		SourceId:   body.SourceId,
	})
	if err != nil {
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

	state, err := h.get.Execute(r.Context(), userId)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to get queue state")
		return
	}
	if state == nil {
		httputil.WriteJSON(w, http.StatusOK, queueStateResponse{
			TrackIds:   []string{},
			RepeatMode: "off",
		})
		return
	}

	trackIds := state.TrackIds
	if trackIds == nil {
		trackIds = []string{}
	}

	httputil.WriteJSON(w, http.StatusOK, queueStateResponse{
		TrackIds:   trackIds,
		CurrentIdx: state.CurrentIdx,
		PositionMs: state.PositionMs,
		Shuffled:   state.Shuffled,
		RepeatMode: state.RepeatMode.String(),
		SourceId:   state.SourceId,
	})
}
