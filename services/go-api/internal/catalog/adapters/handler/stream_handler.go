package handler

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
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

	w.Header().Set("Content-Type", audioContentType(*out.Track.AudioRef))
	http.ServeContent(w, r, "", time.Time{}, out.Reader)
}

// audioContentType maps a stored audio ref to its MIME type. The serve path must
// label the container correctly — iOS/expo-audio decodes progressive audio by the
// Content-Type, so an m4a sent as audio/mpeg fails to play. Mirrors the mapping the
// storage adapter sets on upload. Defaults to audio/mpeg for legacy mp3 refs.
func audioContentType(audioRef string) string {
	switch {
	case strings.HasSuffix(audioRef, ".m4a"):
		return "audio/mp4"
	case strings.HasSuffix(audioRef, ".opus"):
		return "audio/opus"
	case strings.HasSuffix(audioRef, ".ogg"):
		return "audio/ogg"
	default:
		return "audio/mpeg"
	}
}
