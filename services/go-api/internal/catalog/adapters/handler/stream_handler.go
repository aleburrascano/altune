package handler

import (
	"log/slog"
	"net/http"
	"time"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared/httputil"

	"github.com/go-chi/chi/v5"
)

type StreamHandler struct {
	svc *service.StreamTrackService
}

func NewStreamHandler(svc *service.StreamTrackService) *StreamHandler {
	return &StreamHandler{svc: svc}
}

// Routes registers the stream endpoints on r. These paths interleave with the
// /tracks tree, so the handler registers directly onto the shared router rather
// than returning a mountable chi.Router like LibraryHandler/PlaylistHandler/
// TrackHandler. Keeps the paths byte-identical to their previous hand-wiring.
func (h *StreamHandler) Routes(r chi.Router) {
	r.Get("/tracks/{trackId}/audio", h.HandleStreamAudio)
	r.Post("/tracks/{trackId}/audio/recover", h.HandleRecover)
}

func (h *StreamHandler) HandleStreamAudio(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	trackId, err := domain.ParseTrackId(chi.URLParam(r, "trackId"))
	if err != nil {
		httputil.BadRequest(w, "invalid track ID")
		return
	}

	out, err := h.svc.Execute(r.Context(), userId, trackId)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}
	defer out.Reader.Close()

	slog.InfoContext(r.Context(), "stream.serving",
		"track_id", trackId.String(),
		"size_bytes", out.Size,
		"setup_ms", time.Since(start).Milliseconds(),
	)

	w.Header().Set("Content-Type", ports.AudioContentType(*out.Track.AudioRef))
	http.ServeContent(w, r, "", time.Time{}, out.Reader)

	slog.InfoContext(r.Context(), "stream.served",
		"track_id", trackId.String(),
		"total_ms", time.Since(start).Milliseconds(),
	)
}

func (h *StreamHandler) HandleRecover(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	trackId, err := domain.ParseTrackId(chi.URLParam(r, "trackId"))
	if err != nil {
		httputil.BadRequest(w, "invalid track ID")
		return
	}

	if err := h.svc.RecoverIfMissing(r.Context(), userId, trackId); err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
