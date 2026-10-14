package main

import "math"

type verdict string

const (
	verdictApplied         verdict = "applied"
	verdictStale           verdict = "stale"
	verdictISRCChanged     verdict = "skipped-isrc-changed"
	verdictNotReady        verdict = "skipped-not-ready"
	verdictInFlight        verdict = "skipped-in-flight"
	verdictAlreadyEnqueued verdict = "skipped-already-enqueued"
	verdictFileChanged     verdict = "skipped-file-changed"
	verdictDeferred        verdict = "deferred-by-cap"
	verdictEnqueueFailed   verdict = "enqueue-failed"
	verdictFailed          verdict = "failed"
)

type trackRow struct {
	ISRC            *string
	Version         int
	Status          string
	DurationSeconds *float64
	CatalogSeconds  *float64
	Mismatch        bool
	AudioVersion    *string
	InFlight        bool
	PriorReacquire  bool
}

func decide(c change, row trackRow) verdict {
	switch {
	case row.Version != c.Version:
		return verdictStale
	case row.ISRC == nil || *row.ISRC != c.OldISRC:
		return verdictISRCChanged
	case row.Status != "ready":
		return verdictNotReady
	case row.InFlight:
		return verdictInFlight
	case row.DurationSeconds == nil || math.Abs(*row.DurationSeconds-c.FileSeconds) > 1:
		return verdictFileChanged
	case c.Action == actionReacquire && row.PriorReacquire:
		return verdictAlreadyEnqueued
	}
	return verdictApplied
}
