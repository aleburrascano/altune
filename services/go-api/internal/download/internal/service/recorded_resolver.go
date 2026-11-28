package service

import (
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/songfields"
	"context"
	"slices"
)

func NewRecordedResolver(hits []songfields.RecordingIdentity, isrcRecordings []ports.ISRCRecording, pinned *ports.RecordingFacts) *MusicBrainzResolver {
	return MustNewMusicBrainzResolver(ResolverPorts{
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

type recordedSearch []songfields.RecordingIdentity

func (s recordedSearch) SearchRecordings(context.Context, string, int) ([]songfields.RecordingIdentity, error) {
	return slices.Clone(s), nil
}

type recordedISRCLookup []ports.ISRCRecording

func (a recordedISRCLookup) RecordingsByISRC(context.Context, string) ([]ports.ISRCRecording, error) {
	return slices.Clone(a), nil
}
