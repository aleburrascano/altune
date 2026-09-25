package goapi

import (
	"context"
	"time"
)

// observeEvalPath is go-api's operator eval-meter endpoint. It is mounted under the
// observe-guarded "/observe" group (internal/app/observe_wiring.go), so
// the request must carry the read-only bearer the client already attaches.
const observeEvalPath = "/observe/eval"

// observeAcquisitionPath is go-api's operator acquisition-health endpoint, mounted
// under the same observe-guarded "/observe" group.
const observeAcquisitionPath = "/observe/acquisition"

// EvalStatus mirrors go-api's eval-meter status from GET /observe/eval
// against a baseline. Score, Baseline and LastRun are pointers because go-api
// omits them until the meter has run, so a nil pointer means "not scored yet",
// distinct from a real zero score. Unknown fields a newer go-api adds are
// ignored, so the mirror tolerates a version skew rather than failing the read.
type EvalStatus struct {
	Enabled  bool        `json:"enabled"`
	Paused   bool        `json:"paused"`
	State    string      `json:"state"`
	Score    *float64    `json:"score,omitempty"`
	Baseline *float64    `json:"baseline,omitempty"`
	LastRun  *time.Time  `json:"last_run,omitempty"`
	Error    string      `json:"error,omitempty"`
	Queries  []EvalQuery `json:"queries,omitempty"`
}

// EvalQuery is one scored query in the eval meter's last run. Query and Expect
// are watched-app data — escaped only at render time, never trusted as markup.
type EvalQuery struct {
	Query    string `json:"query"`
	Expect   string `json:"expect"`
	Passed   bool   `json:"passed"`
	Position int    `json:"position"`
}

// Scored reports whether the meter has a usable score to render. A nil Score is
// go-api saying "no run yet"; the bucket shows "no score" rather than a spurious
// zero.
func (e EvalStatus) Scored() bool { return e.Score != nil }

// Age reports how long ago the eval meter last ran, measured from now, and whether
// a run time is known. A nil LastRun is go-api saying the meter has never run, so
// there is no age and the second return is false — distinct from a zero-duration
// "just ran".
func (e EvalStatus) Age(now time.Time) (time.Duration, bool) {
	if e.LastRun == nil {
		return 0, false
	}
	return now.Sub(*e.LastRun), true
}

// StaleByAge reports whether the last eval run is older than threshold. It is
// independent of whether the read that fetched the score succeeded: a perfectly
// reachable go-api can still serve a score it computed days ago, and that score is
// stale even though the read is live. A meter that has never run is unscored, not
// stale, so an unknown LastRun is never flagged.
func (e EvalStatus) StaleByAge(now time.Time, threshold time.Duration) bool {
	age, known := e.Age(now)
	return known && age > threshold
}

// AcquisitionStatus mirrors go-api's acquisition-health snapshot from
// GET /observe/acquisition (internal/observe/handler/acquisition.go). Only
// the aggregate counters and in-flight/queue gauges are mirrored — the per-job
// records are go-api's deep operator drill-down, out of this bucket's anchor
// (search-quality score + acquisition success rate). Unknown fields are ignored.
type AcquisitionStatus struct {
	InFlight      int    `json:"in_flight"`
	Succeeded     uint64 `json:"succeeded"`
	Failed        uint64 `json:"failed"`
	Rejected      uint64 `json:"rejected"`
	QueueDepth    int    `json:"queue_depth"`
	QueueCapacity int    `json:"queue_capacity"`
}

// SuccessRateSince is the acquisition success rate over the recent window between
// prev and a — the completions in that window only, succeeded/(succeeded+failed)
// of the delta — not the lifetime average, so a current failure spike is visible
// even while the all-time rate stays high. Rejected admissions are excluded: they
// never ran. With no completed jobs in the window the rate is undefined and the
// second return is false, so the bucket renders "no recent data" rather than a
// misleading 0% or a divide-by-zero.
//
// A counter reset — a go-api restart zeroes the cumulative counters — shows as a
// current count below prev; that is an invalid window (undefined) rather than a
// negative delta, until the window refills post-reset. The deltas feed
// completedRate, which takes the sum in float64 so counters near the uint64
// ceiling cannot wrap it.
func (a AcquisitionStatus) SuccessRateSince(prev AcquisitionStatus) (float64, bool) {
	if a.Succeeded < prev.Succeeded || a.Failed < prev.Failed {
		return 0, false
	}
	succeeded := float64(a.Succeeded - prev.Succeeded)
	failed := float64(a.Failed - prev.Failed)
	return completedRate(succeeded, failed)
}

// completedRate is succeeded/(succeeded+failed) over a set of completed
// acquisitions, and whether it is defined. The sum is taken in float64, not
// uint64: a hostile or corrupt go-api response with counters near the uint64
// ceiling would otherwise wrap the integer sum — wrapping to exactly 0 would
// spuriously report "no data", and a partial wrap would inflate the rate past
// 100%. float64 spans the whole uint64 range without wrapping (it only loses
// precision above 2^53, inconsequential for a percentage and unreachable under any
// benign go-api), and completed is zero only when both counts are genuinely zero,
// so the divide-by-zero guard still holds.
func completedRate(succeeded, failed float64) (float64, bool) {
	completed := succeeded + failed
	if completed == 0 {
		return 0, false
	}
	return succeeded / completed, true
}

// AdminEval fetches GET /observe/eval, go-api's operator eval-meter status,
// decoded into EvalStatus. It reuses the read primitive, so the read-only bearer,
// the host pin, the bounded body and the timeout all apply: an unreachable go-api
// yields a SourceDownError, a rejected token or a principal the admin gate refuses yields
// an APIError, and a runaway body cannot exhaust memory. It is a read; nothing
// here writes, commands or mutates go-api.
func (c *Client) AdminEval(ctx context.Context) (EvalStatus, error) {
	var out EvalStatus
	if err := c.get(ctx, observeEvalPath, &out); err != nil {
		return EvalStatus{}, err
	}
	return out, nil
}

// AdminAcquisition fetches GET /observe/acquisition, go-api's operator
// acquisition-health snapshot, decoded into AcquisitionStatus. It shares the read
// primitive's guarantees with AdminEval — read-only auth, host pin, bounded body,
// timeout — and is likewise a pure read.
func (c *Client) AdminAcquisition(ctx context.Context) (AcquisitionStatus, error) {
	var out AcquisitionStatus
	if err := c.get(ctx, observeAcquisitionPath, &out); err != nil {
		return AcquisitionStatus{}, err
	}
	return out, nil
}
