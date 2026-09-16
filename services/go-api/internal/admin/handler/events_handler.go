package handler

import (
	"altune/go-api/internal/admin/eventtap"
	"altune/go-api/internal/shared/httputil"
	"net/http"
)

// eventRatesResponse is the /events/rates body: per-type event counts over the
// feed's rate window, plus the tap's cumulative dropped-event count so
// operators can tell when the live stream is incomplete.
type eventRatesResponse struct {
	Rates   map[string]int `json:"rates"`
	Dropped uint64         `json:"dropped"`
}

func (h *AdminHandler) serveEventRates(w http.ResponseWriter, _ *http.Request) {
	if h.eventFeed == nil {
		httputil.WriteJSON(w, http.StatusOK, eventRatesResponse{Rates: map[string]int{}})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, eventRatesResponse{
		Rates:   h.eventFeed.Rates(),
		Dropped: h.eventFeed.Dropped(),
	})
}

func (h *AdminHandler) streamEvents(w http.ResponseWriter, r *http.Request) {
	if h.eventFeed == nil {
		httputil.InternalError(w, "event feed unavailable")
		return
	}
	ch, cancel, err := h.eventFeed.Subscribe()
	if err != nil {
		rejectSubscription(w, r, "events", err, eventtap.ErrTooManySubscribers)
		return
	}
	defer cancel()
	streamSSE(w, r, ch)
}
