package ports

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/downloadprovenance"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/songfields"
	"altune/go-api/internal/shared/tracklength"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type JobReporter interface {
	Meta(title, artist, album string)
	Stage(ctx context.Context, name string)
	Source(url string)
	Provenance(value string)
	Timing(stage string, d time.Duration)
	Fetched(format SourceFormat)
	Stored(file StoredFile)
	Decided(confidence float64, sourceKey string, target, measured float64)
}

type AcquisitionJob struct {
	UserID  ids.UserId
	TrackID ids.TrackId

	Attempt        int
	Claim          ClaimNumber
	SchedulerOwned bool

	Reporter    JobReporter
	CommitCheck func(ctx context.Context) error
}

type quietReporter struct{}

func (quietReporter) Meta(_, _, _ string)                       {}
func (quietReporter) Stage(_ context.Context, _ string)         {}
func (quietReporter) Source(_ string)                           {}
func (quietReporter) Provenance(_ string)                       {}
func (quietReporter) Timing(_ string, _ time.Duration)          {}
func (quietReporter) Fetched(_ SourceFormat)                    {}
func (quietReporter) Stored(_ StoredFile)                       {}
func (quietReporter) Decided(_ float64, _ string, _, _ float64) {}

func (j AcquisitionJob) Progress() JobReporter {
	if j.Reporter == nil {
		return quietReporter{}
	}
	return j.Reporter
}

func (j AcquisitionJob) VerifyCommit(ctx context.Context) error {
	if j.CommitCheck == nil {
		return nil
	}
	return j.CommitCheck(ctx)
}

func (j AcquisitionJob) CancelledByScheduler(ctx context.Context) bool {
	return j.SchedulerOwned && ctx.Err() != nil
}

func (j AcquisitionJob) HandedOverByScheduler(ctx context.Context) bool {
	return j.SchedulerOwned && errors.Is(ctx.Err(), context.Canceled)
}

const (
	UnplayableDRM     = "drm"
	UnplayablePreview = "preview"
)

type CandidateKind string

const (
	KindArtTrack    CandidateKind = "art_track"
	KindPinnedVideo CandidateKind = "pinned_video"
	KindUpload      CandidateKind = "upload"
	KindCatalogPin  CandidateKind = "catalog_pin"
)

type AudioCandidate struct {
	Title       string
	Duration    float64
	URL         string
	Channel     string
	Categories  []string
	ViewCount   int64
	Source      domain.SourceName
	Explicit    bool
	Unplayable  string
	Artists     []string
	Album       string
	Kind        CandidateKind
	ObservedOMV bool
}

func (c AudioCandidate) IsResolved() bool {
	return c.Kind != "" && c.Kind != KindUpload
}

func (c AudioCandidate) HasSourceKey() bool {
	return domain.SourceKey(c.URL) != ""
}

func DedupeCandidatesBySourceKey(merged, results []AudioCandidate, positionByKey map[string]int) []AudioCandidate {
	for _, c := range results {
		if !c.HasSourceKey() {
			continue
		}
		key := domain.SourceKey(c.URL)
		merged = mergeCandidate(merged, c, key, positionByKey)
	}
	return merged
}

func mergeCandidate(merged []AudioCandidate, c AudioCandidate, key string, positionByKey map[string]int) []AudioCandidate {
	position, isDuplicate := positionByKey[key]
	if !isDuplicate {
		positionByKey[key] = len(merged)
		return append(merged, c)
	}
	if c.IsResolved() && !merged[position].IsResolved() {
		merged[position] = c
	}
	return merged
}

func CollectCandidates(
	n int,
	run func(i int) ([]AudioCandidate, error),
	onSuccess func(i int, candidates []AudioCandidate),
	onFailure func(i int, err error),
	allFailed func(firstErr error) error,
) ([]AudioCandidate, error) {
	merged, _, err := CollectCandidatesReportingOutage(n, run, onSuccess, onFailure, allFailed)
	return merged, err
}

func CollectCandidatesReportingOutage(
	n int,
	run func(i int) ([]AudioCandidate, error),
	onSuccess func(i int, candidates []AudioCandidate),
	onFailure func(i int, err error),
	allFailed func(firstErr error) error,
) (merged []AudioCandidate, outage, err error) {
	positionByKey := make(map[string]int)
	var failed searchFailures

	for i := 0; i < n; i++ {
		candidates, err := run(i)
		if err != nil {
			failed.note(err)
			onFailure(i, err)
			continue
		}
		onSuccess(i, candidates)
		merged = DedupeCandidatesBySourceKey(merged, candidates, positionByKey)
	}

	if failed.count == n && failed.first != nil {
		return nil, failed.firstUnavailable, allFailed(failed.first)
	}
	if len(merged) == 0 && failed.firstUnavailable != nil {
		return nil, failed.firstUnavailable, fmt.Errorf("no candidates and a source was unavailable: %w", failed.firstUnavailable)
	}
	if len(merged) == 0 && failed.first != nil {
		return nil, nil, degraded(failed.first)
	}
	return merged, failed.firstUnavailable, nil
}

type searchFailures struct {
	count            int
	first            error
	firstUnavailable error
}

func (f *searchFailures) note(err error) {
	f.count++
	if f.first == nil {
		f.first = err
	}
	if f.firstUnavailable == nil && domain.IsSourceUnavailable(err) {
		f.firstUnavailable = err
	}
}

func degraded(err error) error {
	return fmt.Errorf("%w: %w", domain.ErrSearchDegraded, err)
}

type CooldownKind string

const (
	CooldownRetry     CooldownKind = "retry"
	CooldownReacquire CooldownKind = "reacquire"
)

type CooldownStore interface {
	Reserve(ctx context.Context, trackID ids.TrackId, kind CooldownKind, cooldown time.Duration) (at time.Time, ok bool, wait time.Duration, err error)
	Release(ctx context.Context, trackID ids.TrackId, kind CooldownKind, at time.Time) error
}

type ReleaseGroup struct {
	MBID           string
	Title          string
	Type           string
	SecondaryTypes []string
}

type LinkedRecording struct {
	MBID          string
	Title         string
	Artists       []string
	Duration      float64
	ReleaseGroups []ReleaseGroup
}

type AcoustIDResult struct {
	ID         string
	Score      float64
	Recordings []LinkedRecording
}

type RecordingMatch struct {
	AcoustID string
	MBIDs    []string
	Score    float64
	Results  []AcoustIDResult
}

func (m RecordingMatch) Known() bool { return len(m.MBIDs) > 0 || m.AcoustID != "" }

type AudioIdentifier interface {
	Identify(ctx context.Context, filePath string, fullLengthForPreview float64) (RecordingMatch, error)
}

type TrackStatus int

const (
	TrackPending TrackStatus = iota
	TrackReady
	TrackFailed
)

var (
	ErrIllegalTransition = errors.New("illegal acquisition status transition")
	ErrTrackNotFound     = errors.New("track not found")
)

type CatalogSource struct {
	Provider   string
	ExternalID string
	URL        string
}

type TrackSnapshot struct {
	ID                     ids.TrackId
	UserId                 ids.UserId
	Title                  string
	Artist                 string
	Album                  string
	CatalogDurationSeconds *float64
	CatalogMBID            *string
	CatalogExplicit        *bool
	CatalogSources         []CatalogSource
	ISRC                   *string
	Year                   *int
	TrackNumber            *int
	AlbumArtist            *string
	Genre                  *string
	AudioRef               *string
	AudioSourceURL         *string
	StoredDurationSeconds  *float64
	RejectedSourceKeys     []string
	Evidence               json.RawMessage
	Status                 TrackStatus
	Streamable             bool
	Version                int
}

type TrackLabel struct {
	MBID            string
	Title           string
	Album           string
	Year            int
	ISRC            string
	DurationSeconds float64
}

type Completion struct {
	AudioRef           string
	ReplaceAudio       bool
	Duration           tracklength.TrackLength
	ISRC               string
	Provenance         downloadprovenance.AcquisitionProvenance
	RejectedSourceKeys []string
	SourceURL          string
	Confidence         float64
	Evidence           json.RawMessage
	Relabel            *TrackLabel
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
	Get(ctx context.Context, id ids.TrackId, userId ids.UserId) (*TrackSnapshot, error)
	Complete(ctx context.Context, id ids.TrackId, userId ids.UserId, fence ClaimNumber, c Completion) error
	Fail(ctx context.Context, id ids.TrackId, userId ids.UserId, fence ClaimNumber, reason string) error
	RevertAt(ctx context.Context, track TrackSnapshot) error
}

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

func (noopRejectionStore) Active(context.Context, ids.TrackId, time.Time) ([]PriorRejection, error) {
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

type OrphanedAudio struct {
	AudioRef  string
	UserId    ids.UserId
	TrackId   ids.TrackId
	NotBefore time.Time
}

type OrphanedAudioRecorder interface {
	RecordOrphanedAudio(ctx context.Context, orphan OrphanedAudio) error
}

type PreviewTarget struct {
	ISRC          string
	DeezerTrackID string
}

type PreviewComparison struct {
	Matched        bool
	BitErrorRate   float64
	OverlapSeconds float64
	Unavailable    string
}

type PreviewMatcher interface {
	MatchesPreview(ctx context.Context, filePath string, target PreviewTarget) (PreviewComparison, error)
}

type AudioProber interface {
	ProbeDuration(ctx context.Context, filePath string) (float64, error)

	ValidateDecodable(ctx context.Context, filePath string) error
}

type JobKind string

const (
	JobKindAcquire JobKind = "acquire"
	JobKindReplace JobKind = "replace"
	JobKindUpgrade JobKind = "upgrade"
)

type Job struct {
	TrackID     ids.TrackId
	UserID      ids.UserId
	Kind        JobKind
	Run         int
	ClaimNumber ClaimNumber
	Waited      time.Duration

	CorrelationID string
}

type ClaimNumber int

type DownloadQueue interface {
	UpgradeJobQueue
	QueueDepthReader
	Enqueue(ctx context.Context, trackID ids.TrackId, kind JobKind, delay time.Duration) error
	Claim(ctx context.Context, lease time.Duration) (Job, error)
	Heartbeat(ctx context.Context, trackID ids.TrackId, fence ClaimNumber, lease time.Duration) error
	Release(ctx context.Context, trackID ids.TrackId, fence ClaimNumber, delay time.Duration) error
	Settle(ctx context.Context, trackID ids.TrackId, fence ClaimNumber) error
}

type UpgradeJobQueue interface {
	ClaimUpgrade(ctx context.Context, lease time.Duration) (Job, error)
	VerifyLease(ctx context.Context, trackID ids.TrackId, fence ClaimNumber) error
}

type QueueDepthReader interface {
	PendingDepth(ctx context.Context) (pending int, oldestAge time.Duration, err error)
}

type JobNotifier interface {
	Listen(ctx context.Context, wake chan<- struct{})
}

type RecordingProvider string

const (
	ProviderYouTube    RecordingProvider = "youtube"
	ProviderDeezer     RecordingProvider = "deezer"
	ProviderSoundCloud RecordingProvider = "soundcloud"
)

type ProviderRef struct {
	Provider   RecordingProvider
	ExternalID string
	URL        string
}

type RecordingIdentity struct {
	ISRC             string
	MBID             string
	Duration         float64
	Sources          []ProviderRef
	MBIDs            []string
	ReferenceDoubted bool
}

func (r RecordingIdentity) IsZero() bool {
	return r.ISRC == "" && r.MBID == "" && r.Duration == 0 && len(r.Sources) == 0
}

func (r RecordingIdentity) SourceFor(provider RecordingProvider) (ProviderRef, bool) {
	for _, s := range r.Sources {
		if s.Provider == provider {
			return s, true
		}
	}
	return ProviderRef{}, false
}

type RecordingFacts struct {
	MBID           string
	Title          string
	Disambiguation string
	Artists        []string
	ISRCs          []string
	Duration       float64
}

type RecordingLookup interface {
	RecordingByMBID(ctx context.Context, mbid string) (RecordingFacts, bool, error)
}

type RecordingResolver interface {
	Resolve(ctx context.Context, q songfields.RecordingIdentity) (RecordingIdentity, error)
}

func NoopRecordingResolver() RecordingResolver { return noopRecordingResolver{} }

type noopRecordingResolver struct{}

func (noopRecordingResolver) Resolve(context.Context, songfields.RecordingIdentity) (RecordingIdentity, error) {
	return RecordingIdentity{}, nil
}

type RerecordingQuery struct {
	OriginalMBID string
}

type RerecordingFinder interface {
	FindRerecording(ctx context.Context, q RerecordingQuery) (TrackLabel, bool, error)
}

type RecordingSearch interface {
	SearchRecordings(ctx context.Context, query string, limit int) ([]songfields.RecordingIdentity, error)
}

type ISRCRecording struct {
	MBID     string
	Duration float64
}

type ISRCLookup interface {
	RecordingsByISRC(ctx context.Context, isrc string) ([]ISRCRecording, error)
}

type WorkRecording struct {
	MBID           string
	Title          string
	Disambiguation string
	ArtistIDs      []string
	Live           bool
}

type RecordingRelease struct {
	Title  string
	Date   string
	Status string
}

type RecordingDetail struct {
	MBID             string
	Title            string
	ArtistIDs        []string
	ISRCs            []string
	Duration         float64
	FirstReleaseDate string
	Releases         []RecordingRelease
}

type RerecordingLookup interface {
	WorkRecordings(ctx context.Context, recordingMBID string) ([]WorkRecording, error)
	RecordingDetail(ctx context.Context, mbid string) (RecordingDetail, bool, error)
}

type RejectionBasis struct {
	ExpectedSeconds float64
	ReferenceMBID   string
}

type CandidateRejectionRecord struct {
	TrackID   string
	SourceKey string
	Reason    string
	Detail    string
	Basis     RejectionBasis
}

type PriorRejection struct {
	SourceKey string
	Basis     *RejectionBasis
}

type RejectionStore interface {
	Record(ctx context.Context, recs []CandidateRejectionRecord) error
	Active(ctx context.Context, trackID ids.TrackId, since time.Time) ([]PriorRejection, error)
}

type FindRequest struct {
	Title    string
	Artist   string
	Album    string
	ISRC     string
	Duration float64
	Identity RecordingIdentity

	CatalogSources  []CatalogSource
	CatalogExplicit *bool

	KnownSourceURL string

	Tier SourceTier
}

type SourceTier int

const (
	TierFirstThenFallback SourceTier = iota
	TierFallbackOnly
)

func PrefersExplicit(catalogExplicit *bool) bool {
	return catalogExplicit == nil || *catalogExplicit
}

type AudioSource interface {
	Name() domain.SourceName

	Find(ctx context.Context, req FindRequest) ([]AudioCandidate, error)

	Fetch(ctx context.Context, candidate AudioCandidate, outDir string) (FetchedAudio, error)
}

type PreviewingSource interface {
	AudioSource
	PreviewFetcher
}

type FetchedAudio struct {
	Path   string
	Format SourceFormat
}

type SourceFormat struct {
	ID    string
	Codec string
	Kbps  int
}

const MuxedFallbackFormatID = "18"

func (f SourceFormat) IsLowBitrate(floorKbps int) bool {
	if floorKbps <= 0 {
		return false
	}
	return f.ID == MuxedFallbackFormatID || (f.Kbps > 0 && f.Kbps < floorKbps)
}

type PreviewFetcher interface {
	FetchPreview(ctx context.Context, candidate AudioCandidate, outDir string, seconds int) (filePath string, err error)
}

func RunTimedOut(parent, run context.Context) bool {
	return parent.Err() == nil && errors.Is(run.Err(), context.DeadlineExceeded)
}

type Verification struct {
	Ffprobe             bool
	Ffmpeg              bool
	Fpcalc              bool
	YtDlp               bool
	FingerprintVerified bool
}

type JobState string

const (
	JobQueued    JobState = "queued"
	JobRunning   JobState = "running"
	JobSucceeded JobState = "succeeded"
	JobFailed    JobState = "failed"
	JobCancelled JobState = "cancelled"
	JobRetrying  JobState = "retrying"
	JobKept      JobState = "kept"
)

type VerifySkipRecorder interface {
	RecordVerifySkip(gate string)
}

const (
	SkipDecodeTimeout      = "decode_timeout"
	SkipDecoderUnavailable = "decoder_unavailable"
	SkipIdentifyFailed     = "identify_failed"
	SkipPreviewFallback    = "preview_fallback"
	SkipProbeFailed        = "probe_failed"
	SkipNoDuration         = "no_duration"
)

func (v Verification) ToolsPresent() bool {
	return v.Ffprobe && v.Ffmpeg && v.Fpcalc && v.YtDlp
}

type JobRecord struct {
	TrackID        string
	Title          string
	Artist         string
	Album          string
	SourceURL      string
	ResolvedSource string
	State          JobState
	Stage          string
	ScheduledAt    time.Time
	ElapsedMs      int64
	Reason         string
	Provenance     string
	Kind           JobKind
	Run            int
	StageMs        map[string]int64
	Source         SourceFormat
	Stored         StoredFile

	Confidence      float64
	SourceKey       string
	TargetSeconds   float64
	MeasuredSeconds float64
}

type Status struct {
	InFlight         int
	Pending          int
	Succeeded        uint64
	Failed           uint64
	Rejected         uint64
	VerifySkipped    uint64
	Paused           bool
	QueueDepth       int
	OldestPendingAge time.Duration
	QueueDepthKnown  bool
	Workers          int
	Verification     Verification
	ActiveJobs       []JobRecord
	Recent           []JobRecord
}

type AcquisitionOutcome struct {
	TrackID         string
	Outcome         JobState
	Reason          string
	ElapsedMs       int64
	StageMs         map[string]int64
	SourceHost      string
	Source          SourceFormat
	Stored          StoredFile
	Kind            JobKind
	Run             int
	Provenance      string
	Confidence      float64
	SourceKey       string
	TargetSeconds   float64
	MeasuredSeconds float64
}

type StoredFile struct {
	Bytes int64
	Codec string
}

type OutcomeRecorder interface {
	Record(ctx context.Context, o AcquisitionOutcome) error
}

type TrackEvent int

const (
	EventMarkReady TrackEvent = iota
	EventReplaceAudio
	EventMarkFailed
	EventFailPending
	EventRevertToPending
)

func TrackEvents() []TrackEvent {
	return []TrackEvent{EventMarkReady, EventReplaceAudio, EventMarkFailed, EventFailPending, EventRevertToPending}
}

func TrackStatuses() []TrackStatus { return []TrackStatus{TrackPending, TrackReady, TrackFailed} }

func (s TrackStatus) Wire() string {
	switch s {
	case TrackPending:
		return "pending"
	case TrackReady:
		return "ready"
	case TrackFailed:
		return "failed"
	default:
		return "unknown"
	}
}

type StatusRules interface {
	Next(from TrackStatus, event TrackEvent) (TrackStatus, error)
	Claimable() []TrackStatus
	Retryable() []TrackStatus
	Upgradable() []TrackStatus
}

func StatusWires(statuses []TrackStatus) []string {
	wires := make([]string, len(statuses))
	for i, s := range statuses {
		wires[i] = s.Wire()
	}
	return wires
}

type StoredAudioReader interface {
	CopyStored(ctx context.Context, audioRef, dir string) (path string, err error)
}

type TrackTags struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Genre       string
	Year        int
	TrackNumber int
	ISRC        string
}

type AudioTagger interface {
	Tag(ctx context.Context, filePath string, tags TrackTags) error
}

const TempDirPrefix = "altune-acquire-"

type ScratchFolder interface {
	Create(pattern string) (string, error)
	Remove(path string) error
	Size(path string) (int64, error)
	SweepAbandoned(ctx context.Context, abandonedBefore time.Time) int
}

type UpgradeCandidate struct {
	TrackID ids.TrackId
	UserID  ids.UserId
	Grade   string
}

type UpgradeSelection struct {
	Limit      int
	Generation int
	RetryAfter time.Duration
	MaxQueued  int

	CheckInPlace bool

	LowBitrateFloorKbps int

	Upgradable []TrackStatus
}

type UpgradeAttempt struct {
	RejectedSourceKey string
	CountsAsTry       bool
}

type InPlaceRecord struct {
	AudioRef           string
	Generation         int
	Verdict            string
	Basis              string
	Score              float64
	BitErrorRate       float64
	Grade              string
	UserUploadMismatch bool
	MayLower           bool
}

type InPlaceStore interface {
	RecordInPlace(ctx context.Context, trackID ids.TrackId, record InPlaceRecord) (recorded bool, err error)
	NoteInPlaceFailure(ctx context.Context, trackID ids.TrackId) (failures int, err error)
}

type UpgradeStore interface {
	Candidates(ctx context.Context, selection UpgradeSelection) ([]UpgradeCandidate, error)
	NoteAttempt(ctx context.Context, trackID ids.TrackId, attempt UpgradeAttempt) (tries int, err error)
	GiveUp(ctx context.Context, trackID ids.TrackId, generation int, reason string) error
}

var UpgradeEvidenceKeys = []string{
	"upgrade_tried_at", "upgrade_tries",
	"best_available_generation", "best_available_reason", "best_available_at",
	"in_place_generation", "in_place_at", "in_place_basis", "in_place_bit_error_rate", "in_place_failures",
}

const MaxRejectedSourceKeys = 25

type UpgradeStateSnapshot struct {
	Evidence           json.RawMessage
	RejectedSourceKeys []string
}

type EvidenceFate int

const (
	KeepEvidence EvidenceFate = iota
	ResetEvidence
)

type UpgradeState interface {
	State(ctx context.Context, trackID ids.TrackId) (UpgradeStateSnapshot, error)
	SettleAcquisition(ctx context.Context, trackID ids.TrackId, fence ClaimNumber, rejectedSourceKeys []string, evidence EvidenceFate) error
}

func decodeJSONObject(raw json.RawMessage, what string) (map[string]json.RawMessage, error) {
	object := map[string]json.RawMessage{}
	if len(raw) == 0 {
		return object, nil
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("read %s: %w", what, err)
	}
	if object == nil {
		object = map[string]json.RawMessage{}
	}
	return object, nil
}

func MergeUpgradeEvidence(trackEvidence, state json.RawMessage) (json.RawMessage, error) {
	merged, err := decodeJSONObject(trackEvidence, "track evidence")
	if err != nil {
		return nil, err
	}
	for _, key := range UpgradeEvidenceKeys {
		delete(merged, key)
	}
	overlay, err := decodeJSONObject(state, "upgrade state")
	if err != nil {
		return nil, err
	}
	for key, value := range overlay {
		merged[key] = value
	}
	if len(merged) == 0 && len(trackEvidence) == 0 {
		return trackEvidence, nil
	}
	out, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("write merged evidence: %w", err)
	}
	return out, nil
}

func AppendRejectedSourceKeys(existing, added []string) []string {
	out := append([]string(nil), existing...)
	for _, key := range added {
		if key == "" || containsKey(out, key) {
			continue
		}
		out = append(out, key)
		if len(out) > MaxRejectedSourceKeys {
			out = out[len(out)-MaxRejectedSourceKeys:]
		}
	}
	return out
}

func containsKey(keys []string, key string) bool {
	for _, existing := range keys {
		if existing == key {
			return true
		}
	}
	return false
}

type AudioWriter interface {
	Exists(ctx context.Context, audioRef string) (bool, error)
	Store(ctx context.Context, sourcePath string, audioRef string) error
	Delete(ctx context.Context, audioRef string) error
}

type AudioRefLookup interface {
	AudioRefInUse(ctx context.Context, audioRef string, excludeTrackID ids.TrackId) (bool, error)
}
