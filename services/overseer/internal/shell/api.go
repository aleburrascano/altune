package shell

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// bucketsResponse is the GET /api/buckets envelope: every bucket's snapshot,
// ID-sorted (the registry already sorts).
type bucketsResponse struct {
	Buckets []core.Snapshot `json:"buckets"`
}

// handleBuckets returns every bucket's current snapshot as JSON. A bucket that
// panics on Snapshot is contained and reported as a degraded source_down snapshot
// rather than taking down the whole response (degrade-don't-crash).
func (h *Handler) handleBuckets(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, bucketsResponse{Buckets: h.snapshots()})
}

// handleStream is the SSE live channel. It emits every bucket's snapshot on
// connect, then re-emits on a fixed cadence, one `data: <Snapshot JSON>` frame per
// bucket. It is already behind the owner-only guard (verified at connect); the SPA
// reads it with a fetch ReadableStream so it can send the bearer header, and
// reconnects with a refreshed token on a 401. Per-connection work is bounded: each
// tick writes the current snapshots and flushes, holding no growing buffer.
//
// Auth is enforced only at connect: the middleware validates the bearer token when
// the stream is opened and does not re-check it per frame. A token that expires
// mid-stream therefore keeps receiving frames until the connection drops (client
// teardown, network loss, or process exit), at which point the SPA reconnects and
// re-authenticates. This is standard SSE behavior — the stream carries no per-frame
// auth to re-validate against — and is acceptable here: Overseer is single-owner,
// read-only, and exposes only snapshot data the owner is already entitled to see,
// so the window between token expiry and reconnect grants no authority the holder
// lacked at connect. Periodic mid-stream re-validation is deliberately not added; it
// would buy nothing for a single-owner read surface and only add a failure mode
// (a re-check that tears down a live, legitimate stream). Revisit if the stream ever
// carries multi-tenant data or a revocation requirement with a bounded blast radius.
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
	h.emitAll(w, flusher) // initial paint, no wait

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

// emitAll writes one SSE frame per bucket snapshot and flushes. It returns false
// on the first write error (the client went away), so the stream loop exits
// promptly rather than spinning against a dead connection.
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

// snapshots collects every bucket's snapshot, each through the panic-containing
// safeSnapshot, in the registry's stable ID order.
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

const (
	sparkPoints = 30
	sparkWindow = time.Hour
)

func (h *Handler) spark(b core.Bucket, id string) []core.SparkPoint {
	keyed, ok := b.(core.KeySeries)
	if !ok {
		return nil
	}
	name := keyed.KeySeries()
	if name == "" {
		return nil
	}
	to := time.Now().UTC()
	points, err := h.series.Query(id, name, to.Add(-sparkWindow), to)
	if err != nil || len(points) == 0 {
		return nil
	}
	if len(points) > sparkPoints {
		points = points[len(points)-sparkPoints:]
	}
	spark := make([]core.SparkPoint, len(points))
	for i, p := range points {
		spark[i] = core.SparkPoint{At: p.At, V: p.Value}
	}
	return spark
}

// safeSnapshot drives one bucket's Snapshot, converting a panic into a degraded
// source_down snapshot so a single misbehaving bucket cannot take down the whole
// API response or stream (the degrade-don't-crash invariant on the read side, the
// successor to the old safeRender).
func safeSnapshot(b core.Bucket) (snap core.Snapshot) {
	meta := b.Meta()
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("overseer.shell.snapshot_panic", "bucket", meta.ID, "recover", rec)
			snap = core.Snapshot{
				ID:       meta.ID,
				Title:    meta.Title,
				State:    core.StateSourceDown,
				Reason:   goapi.ReasonDown,
				Severity: core.SeverityCritical,
				Headline: "panel unavailable",
				Data:     json.RawMessage(`{"error":"panel unavailable"}`),
			}
		}
	}()
	return b.Snapshot()
}
