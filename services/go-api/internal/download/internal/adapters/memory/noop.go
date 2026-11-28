package memory

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/music"
	"context"
	"time"
)

type noopAudioIdentifier struct{}

func NoopAudioIdentifier() ports.AudioIdentifier { return noopAudioIdentifier{} }

func (noopAudioIdentifier) Identify(context.Context, string, float64) (domain.RecordingMatch, error) {
	return domain.RecordingMatch{}, domain.ErrNoAudioIdentifier
}

type noopRecordingLookup struct{}

func NoopRecordingLookup() ports.RecordingLookup { return noopRecordingLookup{} }

func (noopRecordingLookup) RecordingByMBID(context.Context, string) (domain.RecordingFacts, bool, error) {
	return domain.RecordingFacts{}, false, nil
}

type noopISRCLookup struct{}

func NoopISRCLookup() ports.ISRCLookup { return noopISRCLookup{} }

func (noopISRCLookup) RecordingsByISRC(context.Context, string) ([]domain.ISRCRecording, error) {
	return nil, nil
}

type noopRerecordingLookup struct{}

func NoopRerecordingLookup() ports.RerecordingLookup { return noopRerecordingLookup{} }

func (noopRerecordingLookup) WorkRecordings(context.Context, string) ([]domain.WorkRecording, error) {
	return nil, nil
}

func (noopRerecordingLookup) RecordingDetail(context.Context, string) (domain.RecordingDetail, bool, error) {
	return domain.RecordingDetail{}, false, nil
}

type noopRerecordingFinder struct{}

func NoopRerecordingFinder() ports.RerecordingFinder { return noopRerecordingFinder{} }

func (noopRerecordingFinder) FindRerecording(context.Context, domain.RerecordingQuery) (domain.TrackLabel, bool, error) {
	return domain.TrackLabel{}, false, nil
}

type noopAudioTagger struct{}

func NoopAudioTagger() ports.AudioTagger { return noopAudioTagger{} }

func (noopAudioTagger) Tag(context.Context, string, domain.TrackTags) error { return nil }

type noopRejectionStore struct{}

func NoopRejectionStore() ports.RejectionStore { return noopRejectionStore{} }

func (noopRejectionStore) Record(context.Context, []domain.CandidateRejectionRecord) error {
	return nil
}

func (noopRejectionStore) Active(context.Context, ids.TrackId, time.Time) ([]domain.PriorRejection, error) {
	return nil, nil
}

type noopStoredAudioReader struct{}

func NoopStoredAudioReader() ports.StoredAudioReader { return noopStoredAudioReader{} }

func (noopStoredAudioReader) CopyStored(context.Context, string, string) (string, error) {
	return "", domain.ErrNoStoredAudioReader
}

type noopVerifySkipRecorder struct{}

func NoopVerifySkipRecorder() ports.VerifySkipRecorder { return noopVerifySkipRecorder{} }

func (noopVerifySkipRecorder) RecordVerifySkip(string) {}

const previewMatcherNotWired = "no preview matcher is wired"

type noopPreviewMatcher struct{}

func NoopPreviewMatcher() ports.PreviewMatcher { return noopPreviewMatcher{} }

func (noopPreviewMatcher) MatchesPreview(context.Context, string, domain.PreviewTarget) (domain.PreviewComparison, error) {
	return domain.PreviewComparison{Unavailable: previewMatcherNotWired}, nil
}

type noopRecordingResolver struct{}

func NoopRecordingResolver() ports.RecordingResolver { return noopRecordingResolver{} }

func (noopRecordingResolver) Resolve(context.Context, music.RecordingIdentity) (domain.RecordingIdentity, error) {
	return domain.RecordingIdentity{}, nil
}
