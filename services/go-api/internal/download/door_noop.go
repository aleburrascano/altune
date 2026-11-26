package download

import ports "altune/go-api/internal/download/internal/core/ports"

var (
	NoopAudioIdentifier    = ports.NoopAudioIdentifier
	NoopAudioProber        = ports.NoopAudioProber
	NoopAudioTagger        = ports.NoopAudioTagger
	NoopAudioRefLookup     = ports.NoopAudioRefLookup
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
