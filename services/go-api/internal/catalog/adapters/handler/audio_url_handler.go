package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared/httputil"
)

// maxAudioURLBatch caps how many URLs a single resolve can mint — a queue is not
// unbounded, and the cap keeps one request from signing thousands of objects.
const maxAudioURLBatch = 200

type AudioURLHandler struct {
	svc *service.AudioURLService
}

func NewAudioURLHandler(svc *service.AudioURLService) *AudioURLHandler {
	return &AudioURLHandler{svc: svc}
}

type resolveAudioURLsRequest struct {
	TrackIDs []string `json:"track_ids"`
}

type audioURLDTO struct {
	TrackID   string `json:"track_id"`
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

type resolveAudioURLsResponse struct {
	URLs []audioURLDTO `json:"urls"`
}

// HandleResolve mints short-lived, directly-streamable URLs for the given tracks.
// Tracks it can't sign (unknown, not owned, not ready, or no signer configured)
// are simply absent from the response — the client streams those via the proxy.
func (h *AudioURLHandler) HandleResolve(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	var body resolveAudioURLsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.BadRequest(w, "invalid request body")
		return
	}
	if len(body.TrackIDs) > maxAudioURLBatch {
		httputil.BadRequest(w, "too many track ids")
		return
	}

	ids := make([]domain.TrackId, 0, len(body.TrackIDs))
	for _, raw := range body.TrackIDs {
		id, err := domain.ParseTrackId(raw)
		if err != nil {
			httputil.BadRequest(w, "invalid track id")
			return
		}
		ids = append(ids, id)
	}

	resolved, err := h.svc.Resolve(r.Context(), userId, ids)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	urls := make([]audioURLDTO, 0, len(resolved))
	for _, ru := range resolved {
		urls = append(urls, audioURLDTO{
			TrackID:   ru.TrackID.String(),
			URL:       ru.URL,
			ExpiresAt: ru.ExpiresAt.Format(time.RFC3339),
		})
	}
	slog.InfoContext(r.Context(), "audio_urls.handled",
		"requested", len(ids),
		"resolved", len(urls),
		"duration_ms", time.Since(start).Milliseconds(),
	)
	httputil.WriteJSON(w, http.StatusOK, resolveAudioURLsResponse{URLs: urls})
}
