package musicbrainzmatch

import (
	"altune/go-api/internal/download/internal/core/ports"
	"context"
	"slices"
)

func NewRecorded(hits []ports.SearchedRecording, isrcRecordings []ports.ISRCRecording, pinned *ports.RecordingFacts) *Resolver {
	return New(recordedSearch(hits),
		WithISRCLookup(recordedISRCLookup(isrcRecordings)),
		WithRecordingLookup(recordedLookup{pinned: pinned}))
}

type recordedLookup struct {
	pinned *ports.RecordingFacts
}

func (l recordedLookup) RecordingByMBID(_ context.Context, mbid string) (ports.RecordingFacts, bool, error) {
	if l.pinned == nil || l.pinned.MBID != mbid {
		return ports.RecordingFacts{}, false, nil
	}
	return *l.pinned, true, nil
}

type recordedSearch []ports.SearchedRecording

func (s recordedSearch) SearchRecordings(context.Context, string, int) ([]ports.SearchedRecording, error) {
	return slices.Clone(s), nil
}

type recordedISRCLookup []ports.ISRCRecording

func (a recordedISRCLookup) RecordingsByISRC(context.Context, string) ([]ports.ISRCRecording, error) {
	return slices.Clone(a), nil
}
