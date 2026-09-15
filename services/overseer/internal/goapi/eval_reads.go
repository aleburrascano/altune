package goapi

import (
	"context"
	"time"
)

// adminEvalPath is go-api's operator eval-meter endpoint. It is mounted under the
// operator-guarded "/admin" group (internal/admin/handler/admin_handler.go), so
// the request must carry the operator bearer the client already attaches.
const adminEvalPath = "/admin/eval"

// adminAcquisitionPath is go-api's operator acquisition-health endpoint, mounted
// under the same operator-guarded "/admin" group.
const adminAcquisitionPath = "/admin/acquisition"

// EvalStatus mirrors go-api's eval-meter status from GET /admin/eval
// (internal/admin/evalmeter/meter.go Status): the in-process eval meter scored
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

// AcquisitionStatus mirrors go-api's acquisition-health snapshot from
// GET /admin/acquisition (internal/admin/handler/acquisition_handler.go). Only
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

// SuccessRate is the fraction of completed acquisitions that succeeded,
// succeeded/(succeeded+failed), and whether it is defined. Rejected admissions
// are excluded: they never ran, so counting them would understate the success of
// jobs that actually executed. With no completed jobs the rate is undefined and
// the second return is false, so the bucket renders "no data" rather than a
// misleading 0% or a divide-by-zero.
func (a AcquisitionStatus) SuccessRate() (float64, bool) {
	completed := a.Succeeded + a.Failed
	if completed == 0 {
		return 0, false
	}
	return float64(a.Succeeded) / float64(completed), true
}

// AdminEval fetches GET /admin/eval, go-api's operator eval-meter status,
// decoded into EvalStatus. It reuses the read primitive, so the operator bearer,
// the host pin, the bounded body and the timeout all apply: an unreachable go-api
// yields a SourceDownError, a rejected token or a non-operator principal yields
// an APIError, and a runaway body cannot exhaust memory. It is a read; nothing
// here writes, commands or mutates go-api.
func (c *Client) AdminEval(ctx context.Context) (EvalStatus, error) {
	var out EvalStatus
	if err := c.get(ctx, adminEvalPath, &out); err != nil {
		return EvalStatus{}, err
	}
	return out, nil
}

// AdminAcquisition fetches GET /admin/acquisition, go-api's operator
// acquisition-health snapshot, decoded into AcquisitionStatus. It shares the read
// primitive's guarantees with AdminEval — operator auth, host pin, bounded body,
// timeout — and is likewise a pure read.
func (c *Client) AdminAcquisition(ctx context.Context) (AcquisitionStatus, error) {
	var out AcquisitionStatus
	if err := c.get(ctx, adminAcquisitionPath, &out); err != nil {
		return AcquisitionStatus{}, err
	}
	return out, nil
}
