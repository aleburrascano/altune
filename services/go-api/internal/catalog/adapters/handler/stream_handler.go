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

func (h *StreamHandler) HandleStreamAudio(w http.ResponseWriter, r *http.Request) {
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
	)

	w.Header().Set("Content-Type", ports.AudioContentType(*out.Track.AudioRef))
	http.ServeContent(w, r, "", time.Time{}, out.Reader)
}

// HandleRecover is the client's playback-error hook for presigned streams: since
// those bypass the proxy (and its missing-file recovery), the client calls this
// when a library track fails to play so a genuinely-gone file still gets marked
// failed and re-acquired. Idempotent and a no-op when the file is actually there.
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
