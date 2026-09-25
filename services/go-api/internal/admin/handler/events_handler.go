package handler

import (
	"altune/go-api/internal/admin/eventtap"
	"altune/go-api/internal/shared/httputil"
	"net/http"
	"time"
)

// eventRatesResponse is the /events/rates body: per-type event counts over the
// feed's rate window, plus the tap's cumulative dropped-event count so
// operators can tell when the live stream is incomplete.
type eventRatesResponse struct {
	Rates   map[string]int `json:"rates"`
	Dropped uint64         `json:"dropped"`
}

func (h *AdminHandler) serveEventRates(w http.ResponseWriter, r *http.Request) {
	if !h.hasLiveEventFeed() {
		httputil.HandleServiceError(w, r, errEventFeedUnavailable)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, eventRatesResponse{
		Rates:   h.eventFeed.Rates(),
		Dropped: h.eventFeed.Dropped(),
	})
}

func (h *AdminHandler) streamEvents(w http.ResponseWriter, r *http.Request) {
	if !h.hasLiveEventFeed() {
		httputil.HandleServiceError(w, r, errEventFeedUnavailable)
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

// hasLiveEventFeed reports whether a feed is wired and draining its tap. An
// unwired or unsubscribed feed answers every event route with empty rates and a
// silent stream, which reads exactly like a healthy idle system.
func (h *AdminHandler) hasLiveEventFeed() bool {
	return h.eventFeed != nil && h.eventFeed.Available()
}

type tapEventDTO struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	User      string    `json:"user,omitempty"`
	Subject   string    `json:"subject,omitempty"`
	CorrID    string    `json:"corr_id,omitempty"`
}

func projectTapEvent(ev eventtap.TapEvent) tapEventDTO {
	return tapEventDTO{
		Type:      ev.Type,
		Timestamp: ev.Timestamp,
		User:      userDigest(ev.User),
		Subject:   ev.Subject,
		CorrID:    ev.CorrID,
	}
}
