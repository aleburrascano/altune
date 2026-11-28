package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/music"
	"context"
	"slices"
)

func NewRecordedResolver(hits []music.RecordingIdentity, isrcRecordings []domain.ISRCRecording, pinned *domain.RecordingFacts) *MusicBrainzResolver {
	return MustNewMusicBrainzResolver(ResolverPorts{
		Search:      recordedSearch(hits),
		ISRC:        recordedISRCLookup(isrcRecordings),
		Recording:   recordedLookupOf(pinned),
		Rerecording: recordedRerecordings{},
	})
}

type recordedRerecordings struct{}

func (recordedRerecordings) WorkRecordings(context.Context, string) ([]domain.WorkRecording, error) {
	return nil, nil
}

func (recordedRerecordings) RecordingDetail(context.Context, string) (domain.RecordingDetail, bool, error) {
	return domain.RecordingDetail{}, false, nil
}

type recordedLookup struct {
	facts domain.RecordingFacts
	has   bool
}

func recordedLookupOf(pinned *domain.RecordingFacts) recordedLookup {
	if pinned == nil {
		return recordedLookup{}
	}
	return recordedLookup{facts: *pinned, has: true}
}

func (l recordedLookup) RecordingByMBID(_ context.Context, mbid string) (domain.RecordingFacts, bool, error) {
	if !l.has || l.facts.MBID != mbid {
		return domain.RecordingFacts{}, false, nil
	}
	return l.facts, true, nil
}

type recordedSearch []music.RecordingIdentity

func (s recordedSearch) SearchRecordings(context.Context, string, int) ([]music.RecordingIdentity, error) {
	return slices.Clone(s), nil
}

type recordedISRCLookup []domain.ISRCRecording

func (a recordedISRCLookup) RecordingsByISRC(context.Context, string) ([]domain.ISRCRecording, error) {
	return slices.Clone(a), nil
}
