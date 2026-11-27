package ports

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNoAudioIdentifier   = errors.New("no audio identifier is wired")
	ErrNoAudioProber       = errors.New("no audio prober is wired")
	ErrNoStoredAudioReader = errors.New("no stored-audio reader is wired")
)

type noopAudioIdentifier struct{}

func NoopAudioIdentifier() AudioIdentifier { return noopAudioIdentifier{} }

func (noopAudioIdentifier) Identify(context.Context, string, float64) (RecordingMatch, error) {
	return RecordingMatch{}, ErrNoAudioIdentifier
}

type noopRecordingLookup struct{}

func NoopRecordingLookup() RecordingLookup { return noopRecordingLookup{} }

func (noopRecordingLookup) RecordingByMBID(context.Context, string) (RecordingFacts, bool, error) {
	return RecordingFacts{}, false, nil
}

type noopISRCLookup struct{}

func NoopISRCLookup() ISRCLookup { return noopISRCLookup{} }

func (noopISRCLookup) RecordingsByISRC(context.Context, string) ([]ISRCRecording, error) {
	return nil, nil
}

type noopRerecordingLookup struct{}

func NoopRerecordingLookup() RerecordingLookup { return noopRerecordingLookup{} }

func (noopRerecordingLookup) WorkRecordings(context.Context, string) ([]WorkRecording, error) {
	return nil, nil
}

func (noopRerecordingLookup) RecordingDetail(context.Context, string) (RecordingDetail, bool, error) {
	return RecordingDetail{}, false, nil
}

type noopRerecordingFinder struct{}

func NoopRerecordingFinder() RerecordingFinder { return noopRerecordingFinder{} }

func (noopRerecordingFinder) FindRerecording(context.Context, RerecordingQuery) (TrackLabel, bool, error) {
	return TrackLabel{}, false, nil
}

type noopAudioTagger struct{}

func NoopAudioTagger() AudioTagger { return noopAudioTagger{} }

func (noopAudioTagger) Tag(context.Context, string, TrackTags) error { return nil }

type noopOutcomeRecorder struct{}

func NoopOutcomeRecorder() OutcomeRecorder { return noopOutcomeRecorder{} }

func (noopOutcomeRecorder) Record(context.Context, AcquisitionOutcome) error { return nil }

type noopJobNotifier struct{}

func NoopJobNotifier() JobNotifier { return noopJobNotifier{} }

func (noopJobNotifier) Listen(context.Context, chan<- struct{}) {}

type noopRejectionStore struct{}

func NoopRejectionStore() RejectionStore { return noopRejectionStore{} }

func (noopRejectionStore) Record(context.Context, []CandidateRejectionRecord) error { return nil }

func (noopRejectionStore) Active(context.Context, string, time.Time) ([]PriorRejection, error) {
	return nil, nil
}

type noopStoredAudioReader struct{}

func NoopStoredAudioReader() StoredAudioReader { return noopStoredAudioReader{} }

func (noopStoredAudioReader) CopyStored(context.Context, string, string) (string, error) {
	return "", ErrNoStoredAudioReader
}

type noopVerifySkipRecorder struct{}

func NoopVerifySkipRecorder() VerifySkipRecorder { return noopVerifySkipRecorder{} }

func (noopVerifySkipRecorder) RecordVerifySkip(string) {}

const previewMatcherNotWired = "no preview matcher is wired"

type noopPreviewMatcher struct{}

func NoopPreviewMatcher() PreviewMatcher { return noopPreviewMatcher{} }

func (noopPreviewMatcher) MatchesPreview(context.Context, string, PreviewTarget) (PreviewComparison, error) {
	return PreviewComparison{Unavailable: previewMatcherNotWired}, nil
}
