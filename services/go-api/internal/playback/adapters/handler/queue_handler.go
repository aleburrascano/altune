package handler

import (
	"encoding/json"
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
	TrackIds     []string        `json:"track_ids"`
	CurrentIdx   int             `json:"current_index"`
	PositionMs   int64           `json:"position_ms"`
	Shuffled     bool            `json:"shuffled"`
	RepeatMode   string          `json:"repeat_mode"`
	SourceId     string          `json:"source_id"`
	Source       *queueSourceDTO `json:"source"`
	NaturalOrder []string        `json:"natural_order"`
}

type queueSourceDTO struct {
	Kind       string `json:"kind"`
	PlaylistId string `json:"playlist_id,omitempty"`
	Name       string `json:"name,omitempty"`
	Query      string `json:"query,omitempty"`
}

type queueStateResponse struct {
	TrackIds     []string              `json:"track_ids"`
	CurrentIdx   int                   `json:"current_index"`
	PositionMs   int64                 `json:"position_ms"`
	Shuffled     bool                  `json:"shuffled"`
	RepeatMode   string                `json:"repeat_mode"`
	SourceId     string                `json:"source_id"`
	Source       *queueSourceDTO       `json:"source"`
	NaturalOrder []string              `json:"natural_order"`
	CurrentTrack *currentTrackResponse `json:"current_track,omitempty"`
}

type currentTrackResponse struct {
	Id                string   `json:"id"`
	Title             string   `json:"title"`
	Artist            string   `json:"artist"`
	ArtworkURL        *string  `json:"artwork_url"`
	DurationSeconds   *float64 `json:"duration_seconds"`
	AcquisitionStatus string   `json:"acquisition_status"`
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
		TrackIds:     body.TrackIds,
		CurrentIdx:   body.CurrentIdx,
		PositionMs:   body.PositionMs,
		Shuffled:     body.Shuffled,
		RepeatMode:   body.RepeatMode,
		SourceId:     saveSourceId(body),
		NaturalOrder: body.NaturalOrder,
	})
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *QueueHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	view, err := h.svc.ResumeView(r.Context(), userId)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, toResponse(view))
}

func toResponse(view *service.ResumeView) queueStateResponse {
	state := view.State
	resp := queueStateResponse{
		TrackIds:     state.TrackIds,
		CurrentIdx:   state.CurrentIdx,
		PositionMs:   state.PositionMs,
		Shuffled:     state.Shuffled,
		RepeatMode:   state.RepeatMode.String(),
		SourceId:     state.SourceId,
		Source:       sourceToDTO(domain.ParseQueueSource(state.SourceId)),
		NaturalOrder: state.NaturalOrder,
	}
	if c := view.CurrentTrack; c != nil {
		resp.CurrentTrack = &currentTrackResponse{
			Id:                c.Id,
			Title:             c.Title,
			Artist:            c.Artist,
			ArtworkURL:        c.ArtworkURL,
			DurationSeconds:   c.DurationSeconds,
			AcquisitionStatus: c.AcquisitionStatus,
		}
	}
	return resp
}

func saveSourceId(body saveQueueRequest) string {
	if body.Source != nil {
		return sourceFromDTO(body.Source).Format()
	}
	return body.SourceId
}

func sourceFromDTO(dto *queueSourceDTO) domain.QueueSource {
	if dto == nil {
		return domain.QueueSource{}
	}
	return domain.QueueSource{
		Kind:       dto.Kind,
		PlaylistId: dto.PlaylistId,
		Name:       dto.Name,
		Query:      dto.Query,
	}
}

func sourceToDTO(source domain.QueueSource) *queueSourceDTO {
	if source.IsZero() {
		return nil
	}
	return &queueSourceDTO{
		Kind:       source.Kind,
		PlaylistId: source.PlaylistId,
		Name:       source.Name,
		Query:      source.Query,
	}
}
