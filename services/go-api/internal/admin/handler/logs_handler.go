package handler

import (
	"altune/go-api/internal/shared/logging"
	"net/http"
)

func (h *AdminHandler) streamLogs(w http.ResponseWriter, r *http.Request) {
	ch, cancel, err := h.logRing.Subscribe()
	if err != nil {
		rejectSubscription(w, r, "logs", err, logging.ErrTooManySubscribers)
		return
	}
	defer cancel()
	ctx, stop := h.untilShutdown(r.Context())
	defer stop()
	streamSSE(w, r.WithContext(ctx), ch)
}
