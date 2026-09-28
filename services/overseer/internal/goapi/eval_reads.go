package goapi

import (
	"context"
	"time"
)

const observeEvalPath = "/observe/eval"

const observeAcquisitionPath = "/observe/acquisition"

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

type EvalQuery struct {
	Query    string `json:"query"`
	Expect   string `json:"expect"`
	Passed   bool   `json:"passed"`
	Position int    `json:"position"`
}

func (e EvalStatus) Scored() bool { return e.Score != nil }

func (e EvalStatus) Age(now time.Time) (time.Duration, bool) {
	if e.LastRun == nil {
		return 0, false
	}
	return now.Sub(*e.LastRun), true
}

func (e EvalStatus) StaleByAge(now time.Time, threshold time.Duration) bool {
	age, known := e.Age(now)
	return known && age > threshold
}

type AcquisitionStatus struct {
	InFlight      int    `json:"in_flight"`
	Succeeded     uint64 `json:"succeeded"`
	Failed        uint64 `json:"failed"`
	Rejected      uint64 `json:"rejected"`
	QueueDepth    int    `json:"queue_depth"`
	QueueCapacity int    `json:"queue_capacity"`
}

func (a AcquisitionStatus) SuccessRateSince(prev AcquisitionStatus) (float64, bool) {
	if a.Succeeded < prev.Succeeded || a.Failed < prev.Failed {
		return 0, false
	}
	succeeded := float64(a.Succeeded - prev.Succeeded)
	failed := float64(a.Failed - prev.Failed)
	return completedRate(succeeded, failed)
}

func completedRate(succeeded, failed float64) (float64, bool) {
	completed := succeeded + failed
	if completed == 0 {
		return 0, false
	}
	return succeeded / completed, true
}

func (c *Client) AdminEval(ctx context.Context) (EvalStatus, error) {
	var out EvalStatus
	if err := c.get(ctx, observeEvalPath, &out); err != nil {
		return EvalStatus{}, err
	}
	return out, nil
}

func (c *Client) AdminAcquisition(ctx context.Context) (AcquisitionStatus, error) {
	var out AcquisitionStatus
	if err := c.get(ctx, observeAcquisitionPath, &out); err != nil {
		return AcquisitionStatus{}, err
	}
	return out, nil
}
