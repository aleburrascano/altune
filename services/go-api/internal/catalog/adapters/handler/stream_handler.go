package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

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
		switch {
		case errors.Is(err, service.ErrTrackNotFound):
			httputil.NotFound(w, "track not found")
		case errors.Is(err, service.ErrAudioNotAvailable):
			httputil.NotFound(w, "audio not available")
		default:
			slog.ErrorContext(r.Context(), "stream track failed", "error", err)
			httputil.InternalError(w)
		}
		return
	}
	defer out.Reader.Close()

	slog.InfoContext(r.Context(), "stream.serving",
		"track_id", trackId.String(),
		"size_bytes", out.Size,
	)

	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Content-Length", strconv.FormatInt(out.Size, 10))
	w.Header().Set("Accept-Ranges", "bytes")
	w.WriteHeader(http.StatusOK)

	buf := make([]byte, 32*1024)
	for {
		if r.Context().Err() != nil {
			slog.InfoContext(r.Context(), "stream.client_disconnected",
				"track_id", trackId.String())
			return
		}
		n, readErr := out.Reader.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				slog.WarnContext(r.Context(), "stream.write_error",
					"track_id", trackId.String(), "error", writeErr)
				return
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				slog.WarnContext(r.Context(), "stream.read_error",
					"track_id", trackId.String(), "error", readErr)
			}
			return
		}
	}
}
