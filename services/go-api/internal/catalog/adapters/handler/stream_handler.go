package handler

import (
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared/httputil"

	"github.com/go-chi/chi/v5"
)

type StreamHandler struct {
	trackRepo  ports.TrackRepository
	audioStore ports.AudioStore
	reconcile  *service.ReconcileTrackStatusService
	scheduler  acquisitionScheduler
}

func NewStreamHandler(
	trackRepo ports.TrackRepository,
	audioStore ports.AudioStore,
	reconcile *service.ReconcileTrackStatusService,
	scheduler acquisitionScheduler,
) *StreamHandler {
	return &StreamHandler{
		trackRepo:  trackRepo,
		audioStore: audioStore,
		reconcile:  reconcile,
		scheduler:  scheduler,
	}
}

func (h *StreamHandler) HandleStreamAudio(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	trackIdStr := chi.URLParam(r, "trackId")

	trackId, err := domain.ParseTrackId(trackIdStr)
	if err != nil {
		httputil.BadRequest(w, "invalid track ID")
		return
	}

	track, err := h.trackRepo.GetByID(r.Context(), trackId, userId)
	if err != nil {
		httputil.InternalError(w)
		return
	}
	if track == nil {
		httputil.NotFound(w, "track not found")
		return
	}

	if !track.IsStreamable() {
		httputil.NotFound(w, "audio not available")
		return
	}

	slog.InfoContext(r.Context(), "stream.start",
		"track_id", trackId.String(),
		"title", track.Title,
		"artist", track.Artist,
		"audio_ref", *track.AudioRef,
	)

	reader, size, err := h.audioStore.Stream(r.Context(), *track.AudioRef)
	if err != nil {
		slog.WarnContext(r.Context(), "stream.failed",
			"track_id", trackId.String(), "error", err)

		_ = h.reconcile.Execute(r.Context(), userId, trackId)

		if h.scheduler != nil {
			slog.InfoContext(r.Context(), "stream.reacquire_scheduled",
				"track_id", trackId.String())
			h.scheduler.Schedule(userId, trackId)
		}

		httputil.NotFound(w, "audio file not found")
		return
	}
	defer reader.Close()

	slog.InfoContext(r.Context(), "stream.serving",
		"track_id", trackId.String(),
		"size_bytes", size,
	)

	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Accept-Ranges", "bytes")
	w.WriteHeader(http.StatusOK)

	buf := make([]byte, 32*1024)
	for {
		if r.Context().Err() != nil {
			slog.InfoContext(r.Context(), "stream.client_disconnected",
				"track_id", trackId.String())
			return
		}
		n, readErr := reader.Read(buf)
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
