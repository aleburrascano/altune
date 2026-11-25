package httpapi

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"altune/overseer/internal/panicguard"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type bucketsResponse struct {
	Buckets []core.Snapshot `json:"buckets"`
}

func (h *Handler) handleBuckets(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, bucketsResponse{Buckets: h.snapshots()})
}

func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ctx := r.Context()
	h.emitAll(w, flusher)

	ticker := time.NewTicker(h.streamInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !h.emitAll(w, flusher) {
				return
			}
		}
	}
}

func (h *Handler) emitAll(w http.ResponseWriter, flusher http.Flusher) bool {
	for _, snap := range h.snapshots() {
		payload, err := json.Marshal(snap)
		if err != nil {
			continue
		}
		if _, err := w.Write([]byte("data: ")); err != nil {
			return false
		}
		if _, err := w.Write(payload); err != nil {
			return false
		}
		if _, err := w.Write([]byte("\n\n")); err != nil {
			return false
		}
	}
	flusher.Flush()
	return true
}

func (h *Handler) snapshots() []core.Snapshot {
	buckets := h.registry.Buckets()
	out := make([]core.Snapshot, 0, len(buckets))
	for _, b := range buckets {
		snap := safeSnapshot(b)
		snap.Spark = h.spark(b, snap.ID)
		out = append(out, snap)
	}
	return out
}

func safeSnapshot(b core.Bucket) core.Snapshot {
	meta := b.Meta()
	var snap core.Snapshot
	panicked := true
	panicguard.Contain(context.Background(), "overseer.shell.snapshot_panic", func() {
		snap = b.Snapshot()
		panicked = false
	}, "bucket", meta.ID)
	if !panicked {
		return snap
	}
	return core.Snapshot{
		ID:       meta.ID,
		Title:    meta.Title,
		State:    core.StateSourceDown,
		Reason:   goapi.ReasonDown,
		Severity: core.SeverityCritical,
		Headline: "panel unavailable",
		Data:     json.RawMessage(`{"error":"panel unavailable"}`),
	}
}
