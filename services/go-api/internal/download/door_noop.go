package download

import "altune/go-api/internal/download/internal/ports"

var (
	NoopAudioIdentifier    = ports.NoopAudioIdentifier
	NoopAudioTagger        = ports.NoopAudioTagger
	NoopPreviewMatcher     = ports.NoopPreviewMatcher
	NoopJobNotifier        = ports.NoopJobNotifier
	NoopOutcomeRecorder    = ports.NoopOutcomeRecorder
	NoopRecordingLookup    = ports.NoopRecordingLookup
	NoopRecordingResolver  = ports.NoopRecordingResolver
	NoopRejectionStore     = ports.NoopRejectionStore
	NoopRerecordingFinder  = ports.NoopRerecordingFinder
	NoopStoredAudioReader  = ports.NoopStoredAudioReader
	NoopVerifySkipRecorder = ports.NoopVerifySkipRecorder
)
