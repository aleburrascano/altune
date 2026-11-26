package musicbrainzmatch

import (
	"altune/go-api/internal/download/internal/core/ports"
	"context"
	"slices"
)

func NewRecorded(hits []ports.SearchedRecording, isrcRecordings []ports.ISRCRecording, pinned *ports.RecordingFacts) *Resolver {
	return MustNew(Ports{
		Search:      recordedSearch(hits),
		ISRC:        recordedISRCLookup(isrcRecordings),
		Recording:   recordedLookupOf(pinned),
		Rerecording: ports.NoopRerecordingLookup(),
	})
}

type recordedLookup struct {
	facts ports.RecordingFacts
	has   bool
}

func recordedLookupOf(pinned *ports.RecordingFacts) recordedLookup {
	if pinned == nil {
		return recordedLookup{}
	}
	return recordedLookup{facts: *pinned, has: true}
}

func (l recordedLookup) RecordingByMBID(_ context.Context, mbid string) (ports.RecordingFacts, bool, error) {
	if !l.has || l.facts.MBID != mbid {
		return ports.RecordingFacts{}, false, nil
	}
	return l.facts, true, nil
}

type recordedSearch []ports.SearchedRecording

func (s recordedSearch) SearchRecordings(context.Context, string, int) ([]ports.SearchedRecording, error) {
	return slices.Clone(s), nil
}

type recordedISRCLookup []ports.ISRCRecording

func (a recordedISRCLookup) RecordingsByISRC(context.Context, string) ([]ports.ISRCRecording, error) {
	return slices.Clone(a), nil
}
