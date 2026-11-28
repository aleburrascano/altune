package ports

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/songfields"
	"context"
	"encoding/json"
	"time"
)

type EvidenceCodec interface {
	Encode(evidence domain.AcquisitionEvidence) (json.RawMessage, error)
	Decode(raw json.RawMessage) (domain.AcquisitionEvidence, error)
	DecodeInPlace(raw json.RawMessage) (domain.InPlaceEvidence, error)
}

type CooldownStore interface {
	Reserve(ctx context.Context, trackID ids.TrackId, kind domain.CooldownKind, cooldown time.Duration) (at time.Time, ok bool, wait time.Duration, err error)
	Release(ctx context.Context, trackID ids.TrackId, kind domain.CooldownKind, at time.Time) error
}

type AudioIdentifier interface {
	Identify(ctx context.Context, filePath string, fullLengthForPreview float64) (domain.RecordingMatch, error)
}

type TrackRequeuer interface {
	Requeue(ctx context.Context, id ids.TrackId) error
}

type QueueTracks interface {
	TrackRequeuer
	StatusRules
}

type LibraryTracks interface {
	AudioRefLookup
	QueueTracks
	Get(ctx context.Context, id ids.TrackId, userId ids.UserId) (*domain.TrackSnapshot, error)
	Complete(ctx context.Context, id ids.TrackId, userId ids.UserId, fence domain.ClaimNumber, c domain.Completion) error
	Fail(ctx context.Context, id ids.TrackId, userId ids.UserId, fence domain.ClaimNumber, reason string) error
	RevertAt(ctx context.Context, track domain.TrackSnapshot) error
}

type TrackLibrary interface {
	LibraryTracks
	EvidenceAccepted(c domain.Completion) bool
}

type StatusRules interface {
	Next(from domain.TrackStatus, event domain.TrackEvent) (domain.TrackStatus, error)
	Claimable() []domain.TrackStatus
	Retryable() []domain.TrackStatus
	Upgradable() []domain.TrackStatus
}

type OrphanedAudioRecorder interface {
	RecordOrphanedAudio(ctx context.Context, orphan domain.OrphanedAudio) error
}

type PreviewMatcher interface {
	MatchesPreview(ctx context.Context, filePath string, target domain.PreviewTarget) (domain.PreviewComparison, error)
}

type AudioProber interface {
	ProbeDuration(ctx context.Context, filePath string) (float64, error)

	ValidateDecodable(ctx context.Context, filePath string) error
}

type DownloadQueue interface {
	UpgradeJobQueue
	QueueDepthReader
	Enqueue(ctx context.Context, trackID ids.TrackId, kind domain.JobKind, delay time.Duration) error
	Claim(ctx context.Context, lease time.Duration) (domain.Job, error)
	Heartbeat(ctx context.Context, trackID ids.TrackId, fence domain.ClaimNumber, lease time.Duration) error
	Release(ctx context.Context, trackID ids.TrackId, fence domain.ClaimNumber, delay time.Duration) error
	Settle(ctx context.Context, trackID ids.TrackId, fence domain.ClaimNumber) error
}

type UpgradeJobQueue interface {
	ClaimUpgrade(ctx context.Context, lease time.Duration) (domain.Job, error)
	VerifyLease(ctx context.Context, trackID ids.TrackId, fence domain.ClaimNumber) error
}

type QueueDepthReader interface {
	PendingDepth(ctx context.Context) (pending int, oldestAge time.Duration, err error)
}

type JobNotifier interface {
	Listen(ctx context.Context, wake chan<- struct{})
}

type RecordingLookup interface {
	RecordingByMBID(ctx context.Context, mbid string) (domain.RecordingFacts, bool, error)
}

type RecordingResolver interface {
	Resolve(ctx context.Context, q songfields.RecordingIdentity) (domain.RecordingIdentity, error)
}

type RerecordingFinder interface {
	FindRerecording(ctx context.Context, q domain.RerecordingQuery) (domain.TrackLabel, bool, error)
}

type RecordingSearch interface {
	SearchRecordings(ctx context.Context, query string, limit int) ([]songfields.RecordingIdentity, error)
}

type ISRCLookup interface {
	RecordingsByISRC(ctx context.Context, isrc string) ([]domain.ISRCRecording, error)
}

type RerecordingLookup interface {
	WorkRecordings(ctx context.Context, recordingMBID string) ([]domain.WorkRecording, error)
	RecordingDetail(ctx context.Context, mbid string) (domain.RecordingDetail, bool, error)
}

type RejectionStore interface {
	Record(ctx context.Context, recs []domain.CandidateRejectionRecord) error
	Active(ctx context.Context, trackID ids.TrackId, since time.Time) ([]domain.PriorRejection, error)
}

type AudioSource interface {
	Name() domain.SourceName

	Find(ctx context.Context, req domain.FindRequest) ([]domain.AudioCandidate, error)

	Fetch(ctx context.Context, candidate domain.AudioCandidate, outDir string) (domain.FetchedAudio, error)
}

type PreviewFetcher interface {
	FetchPreview(ctx context.Context, candidate domain.AudioCandidate, outDir string, seconds int) (filePath string, err error)
}

type PreviewingSource interface {
	AudioSource
	PreviewFetcher
}

type SourceOrder struct {
	First      []AudioSource
	Fallback   []AudioSource
	Previewing []PreviewingSource
}

func (t SourceOrder) All() []AudioSource {
	return append(append([]AudioSource{}, t.First...), t.Fallback...)
}

type VerifySkipRecorder interface {
	RecordVerifySkip(gate string)
}

type OutcomeRecorder interface {
	Record(ctx context.Context, o domain.AcquisitionOutcome) error
}

type StoredAudioReader interface {
	CopyStored(ctx context.Context, audioRef, dir string) (path string, err error)
}

type AudioTagger interface {
	Tag(ctx context.Context, filePath string, tags domain.TrackTags) error
}

type ScratchFolder interface {
	Create(pattern string) (string, error)
	Remove(path string) error
	Size(path string) (int64, error)
	SweepAbandoned(ctx context.Context, abandonedBefore time.Time) int
}

type InPlaceStore interface {
	RecordInPlace(ctx context.Context, trackID ids.TrackId, record domain.InPlaceRecord) (recorded bool, err error)
	NoteInPlaceFailure(ctx context.Context, trackID ids.TrackId) (failures int, err error)
}

type InPlaceVerdicts interface {
	RecordInPlaceVerdict(ctx context.Context, trackID ids.TrackId, record domain.InPlaceRecord) (recorded bool, err error)
}

type InPlaceState interface {
	RecordInPlaceState(ctx context.Context, trackID ids.TrackId, generation int, basis string, bitErrorRate float64) error
	NoteInPlaceFailure(ctx context.Context, trackID ids.TrackId) (int, error)
}

type UpgradeStore interface {
	Candidates(ctx context.Context, selection domain.UpgradeSelection) ([]domain.UpgradeCandidate, error)
	NoteAttempt(ctx context.Context, trackID ids.TrackId, attempt domain.UpgradeAttempt) (tries int, err error)
	GiveUp(ctx context.Context, trackID ids.TrackId, generation int, reason string) error
}

type UpgradeState interface {
	State(ctx context.Context, trackID ids.TrackId) (domain.UpgradeStateSnapshot, error)
	SettleAcquisition(ctx context.Context, trackID ids.TrackId, fence domain.ClaimNumber, rejectedSourceKeys []string, evidence domain.EvidenceFate) error
}

type AudioWriter interface {
	Exists(ctx context.Context, audioRef string) (bool, error)
	Store(ctx context.Context, sourcePath string, audioRef string) error
	Delete(ctx context.Context, audioRef string) error
}

type AudioRefLookup interface {
	AudioRefInUse(ctx context.Context, audioRef string, excludeTrackID ids.TrackId) (bool, error)
}
