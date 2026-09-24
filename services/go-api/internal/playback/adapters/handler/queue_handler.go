package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/playback/service"
	"altune/go-api/internal/shared/httputil"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type QueueHandler struct {
	svc     *service.QueueService
	limiter *userRateLimiter
}

type queueHandlerConfig struct {
	rateLimit        QueueStateRateLimit
	now              func() time.Time
	rateLimitMetrics ports.RateLimitMetrics
}

// QueueHandlerOption customises a QueueHandler.
type QueueHandlerOption func(*queueHandlerConfig)

// WithQueueStateRateLimit replaces DefaultQueueStateRateLimit.
func WithQueueStateRateLimit(limit QueueStateRateLimit) QueueHandlerOption {
	return func(c *queueHandlerConfig) { c.rateLimit = limit }
}

// withClock injects the limiter's clock so tests can refill buckets without
// sleeping.
func withClock(now func() time.Time) QueueHandlerOption {
	return func(c *queueHandlerConfig) { c.now = now }
}

func NewQueueHandler(svc *service.QueueService, opts ...QueueHandlerOption) *QueueHandler {
	cfg := queueHandlerConfig{
		rateLimit:        DefaultQueueStateRateLimit,
		now:              time.Now,
		rateLimitMetrics: ports.NoopRateLimitMetrics(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &QueueHandler{svc: svc, limiter: newUserRateLimiter(cfg.rateLimit, cfg.now, cfg.rateLimitMetrics)}
}

// Routes throttles PUT and GET per user (one shared bucket). DELETE is the
// GDPR erasure path and stays unthrottled so a user can always erase.
func (h *QueueHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.With(h.limiter.middleware).Put("/queue-state", h.handleSave)
	r.With(h.limiter.middleware).Put("/queue-state/position", h.handleSavePosition)
	r.With(h.limiter.middleware).Get("/queue-state", h.handleGet)
	r.Delete("/queue-state", h.handleForget)
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

// saveQueuePositionRequest is the position-only body of
// PUT /queue-state/position. It carries no track list, so the frequent
// autosave does not resend and revalidate the whole queue (#1126).
// current_track_id is required: the save applies only when the stored queue
// holds that track at current_index.
type saveQueuePositionRequest struct {
	CurrentIdx     int    `json:"current_index"`
	CurrentTrackId string `json:"current_track_id"`
	PositionMs     int64  `json:"position_ms"`
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
	// CurrentTrackUnavailable is present (true) only when the now-playing
	// lookup failed; an absent current_track without it means no current track.
	CurrentTrackUnavailable bool `json:"current_track_unavailable,omitempty"`
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
	if !httputil.DecodeJSON(w, r, &body) {
		return
	}

	sourceId, err := domain.FormatQueueSource(sourceFromDTO(body.Source), body.SourceId)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	err = h.svc.Save(r.Context(), userId, service.SaveQueueStateInput{
		TrackIds:     body.TrackIds,
		CurrentIdx:   body.CurrentIdx,
		PositionMs:   body.PositionMs,
		Shuffled:     body.Shuffled,
		RepeatMode:   body.RepeatMode,
		SourceId:     sourceId,
		NaturalOrder: body.NaturalOrder,
	})
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleSavePosition is the lighter save for position-only updates. A 409
// with code playback.queue_position_mismatch means no stored queue matches, and
// the client should fall back to a full PUT /queue-state; a 409 with
// playback.stale_queue_write keeps its full-save meaning (#664).
func (h *QueueHandler) handleSavePosition(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	var body saveQueuePositionRequest
	if !httputil.DecodeJSON(w, r, &body) {
		return
	}

	err := h.svc.SavePosition(r.Context(), userId, service.SaveQueuePositionInput{
		CurrentIdx:     body.CurrentIdx,
		CurrentTrackId: body.CurrentTrackId,
		PositionMs:     body.PositionMs,
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

	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	httputil.WriteJSON(w, http.StatusOK, toResponse(view))
}

// handleForget is the self-service GDPR erasure entrypoint: an authenticated
// user erases their own persisted queue state (all PII it holds).
func (h *QueueHandler) handleForget(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	if err := h.svc.Forget(r.Context(), userId); err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
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

		CurrentTrackUnavailable: view.CurrentTrackUnavailable,
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
