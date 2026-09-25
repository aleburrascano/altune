package handler

import (
	"altune/go-api/internal/observe/eventtap"
	"altune/go-api/internal/shared/httputil"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"
)

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
	ctx, stop := h.untilShutdown(r.Context())
	defer stop()
	streamSSE(w, r.WithContext(ctx), ch)
}

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

const userDigestBytes = 4

func userDigest(user string) string {
	if user == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(user))
	return hex.EncodeToString(sum[:userDigestBytes])
}
