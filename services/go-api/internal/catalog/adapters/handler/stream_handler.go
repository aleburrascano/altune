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
}

func NewStreamHandler(
	trackRepo ports.TrackRepository,
	audioStore ports.AudioStore,
	reconcile *service.ReconcileTrackStatusService,
) *StreamHandler {
	return &StreamHandler{
		trackRepo:  trackRepo,
		audioStore: audioStore,
		reconcile:  reconcile,
	}
}

func (h *StreamHandler) HandleStreamAudio(w http.ResponseWriter, r *http.Request) {
	userId := auth.MustUserID(r.Context())
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

	if track.AcquisitionStatus != domain.AcquisitionReady || track.AudioRef == nil {
		httputil.NotFound(w, "audio not available")
		return
	}

	reader, size, err := h.audioStore.Stream(r.Context(), *track.AudioRef)
	if err != nil {
		slog.WarnContext(r.Context(), "audio stream failed, reconciling",
			"track_id", trackId.String(), "error", err)

		_ = h.reconcile.Execute(r.Context(), userId, trackId)
		httputil.NotFound(w, "audio file not found")
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Accept-Ranges", "bytes")
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, reader); err != nil {
		slog.WarnContext(r.Context(), "stream copy interrupted", "error", err)
	}
}
