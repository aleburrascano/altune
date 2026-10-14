package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type AcquireTrackAudioService struct {
	ledger          ports.TrackLedger
	sources         *SourceRegistry
	audioStore      ports.AudioWriter
	audioProber     ports.AudioProber
	audioTagger     ports.AudioTagger
	identifier      ports.AudioIdentifier
	recordings      ports.RecordingResolver
	events          events.Publisher
	orphans         ports.OrphanedAudioRecorder
	storeKeyPrefix  string
	rejections      ports.RejectionStore
	downloadLimiter *DownloadLimiter
	verifySkips     ports.VerifySkipRecorder
	confidenceFloor float64
	workspace       ports.TempWorkspace
}

func NewAcquireTrackAudioService(
	ledger ports.TrackLedger,
	sources *SourceRegistry,
	audioStore ports.AudioWriter,
	opts ...func(*AcquireTrackAudioService),
) *AcquireTrackAudioService {
	s := &AcquireTrackAudioService{
		ledger:     ledger,
		sources:    sources,
		audioStore: audioStore,
		recordings: ports.NoopRecordingResolver(),
		events:     events.NoopPublisher(),
		workspace:  defaultTempWorkspace,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithDownloadLimiter(l *DownloadLimiter) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.downloadLimiter = l }
}

func WithAcquireTempWorkspace(w ports.TempWorkspace) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.workspace = w }
}

func WithAcquireConfidenceFloor(floor float64) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.confidenceFloor = floor }
}

func WithVerifySkips(r ports.VerifySkipRecorder) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.verifySkips = r }
}

func WithAcquireEvents(pub events.Publisher) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) {
		if pub != nil {
			s.events = pub
		}
	}
}

func WithAcquireOrphanQueue(q ports.OrphanedAudioRecorder) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.orphans = q }
}

func WithRecordingResolver(r ports.RecordingResolver) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) {
		if r != nil {
			s.recordings = r
		}
	}
}

func WithAudioProber(p ports.AudioProber) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.audioProber = p }
}

func WithAudioTagger(t ports.AudioTagger) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.audioTagger = t }
}

func WithAudioIdentifier(i ports.AudioIdentifier) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.identifier = i }
}

func WithAcquireStoreKeyPrefix(prefix string) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.storeKeyPrefix = prefix }
}

func WithRejectionStore(r ports.RejectionStore) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.rejections = r }
}

const acquireTimeout = 10 * time.Minute

type schedulerOwnedJobContextKey struct{}

func withSchedulerOwnedJobContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, schedulerOwnedJobContextKey{}, true)
}

type jobAttemptContextKey struct{}

func withJobAttempt(ctx context.Context, attempt int) context.Context {
	return context.WithValue(ctx, jobAttemptContextKey{}, attempt)
}

func jobAttemptFrom(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	attempt, _ := ctx.Value(jobAttemptContextKey{}).(int)
	return attempt
}

func schedulerCancelledJobContext(ctx context.Context) bool {
	ownedByScheduler, _ := ctx.Value(schedulerOwnedJobContextKey{}).(bool)
	return ownedByScheduler && ctx.Err() != nil
}

func (s *AcquireTrackAudioService) Execute(ctx context.Context, userId shared.UserId, trackId shared.TrackId) error {
	jobCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, acquireTimeout)
	defer cancel()
	track, err := s.loadTrack(ctx, userId, trackId)
	if err != nil || track == nil {
		return err
	}
	proceed, err := s.reconcileForReacquire(ctx, track)
	if err != nil || !proceed {
		return err
	}
	ac := s.startAcquisition(ctx, userId, trackId, track)
	defer CleanupTemp(ctx, s.workspace, ac)
	s.loadPriorRejections(ctx, ac)
	if err := s.runAcquisition(ctx, userId, trackId, ac); err != nil {
		return s.settleExecuteFailure(ctx, jobCtx, userId, trackId, err, ac)
	}
	return nil
}

func (s *AcquireTrackAudioService) settleExecuteFailure(ctx, jobCtx context.Context, userId shared.UserId, trackId shared.TrackId, err error, ac *AcquisitionContext) error {
	if schedulerCancelledJobContext(jobCtx) {
		return s.reportSchedulerCancelledWithoutMarkingFailed(ctx, userId, trackId, err)
	}
	if willRetry(jobCtx, err) {
		return s.reportRetryableFailure(ctx, userId, trackId, err)
	}
	return s.reportAcquireFailure(ctx, userId, trackId, err, ac)
}

func (s *AcquireTrackAudioService) ExecuteReplace(ctx context.Context, userId shared.UserId, trackId shared.TrackId) error {
	jobCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, acquireTimeout)
	defer cancel()
	track, err := s.loadTrack(ctx, userId, trackId)
	if err != nil || track == nil {
		return err
	}
	ac := s.startAcquisition(ctx, userId, trackId, track)
	defer CleanupTemp(ctx, s.workspace, ac)
	configureReplaceExclusion(ctx, ac, track, trackId)
	s.loadPriorRejections(ctx, ac)
	if err := s.runAcquisition(ctx, userId, trackId, ac); err != nil {
		return s.settleReplaceFailure(ctx, jobCtx, userId, trackId, err, ac)
	}
	s.deleteSupersededAudio(ctx, userId, trackId, ac)
	return nil
}

func (s *AcquireTrackAudioService) settleReplaceFailure(ctx, jobCtx context.Context, userId shared.UserId, trackId shared.TrackId, err error, ac *AcquisitionContext) error {
	if schedulerCancelledJobContext(jobCtx) {
		return s.reportSchedulerCancelledWithoutMarkingFailed(ctx, userId, trackId, err)
	}
	return s.reportReplaceFailure(ctx, userId, trackId, err, ac)
}

func (s *AcquireTrackAudioService) deleteSupersededAudio(ctx context.Context, userId shared.UserId, trackId shared.TrackId, ac *AcquisitionContext) {
	old := ac.Replace.PreservedRef
	if old == "" || old == ac.AudioRef {
		return
	}
	delCtx, cancel := detachedTimeout(ctx, supersededDeleteBudget)
	defer cancel()
	if s.servedByAnotherTrack(delCtx, trackId, old) {
		return
	}
	if err := s.audioStore.Delete(delCtx, old); err != nil {
		slog.ErrorContext(ctx, "acquisition.replace_orphaned_old_audio",
			"track_id", trackId.String(), "audio_ref", old, "error", redact.LogError(err))
		recordOrphanedAudio(ctx, s.orphans, userId, trackId, old)
	}
}

func (s *AcquireTrackAudioService) servedByAnotherTrack(ctx context.Context, trackId shared.TrackId, audioRef string) bool {
	inUse, err := s.ledger.AudioRefInUse(ctx, audioRef, trackId)
	if err != nil {
		slog.ErrorContext(ctx, "acquisition.replace_audio_usage_unknown",
			"track_id", trackId.String(), "audio_ref", audioRef, "error", redact.LogError(err))
		return true
	}
	if inUse {
		slog.InfoContext(ctx, "acquisition.replace_kept_shared_audio",
			"track_id", trackId.String(), "audio_ref", audioRef)
	}
	return inUse
}

func (s *AcquireTrackAudioService) loadTrack(ctx context.Context, userId shared.UserId, trackId shared.TrackId) (*ports.TrackSnapshot, error) {
	track, err := s.ledger.Get(ctx, trackId, userId)
	if err != nil {
		return nil, fmt.Errorf("get track: %w", err)
	}
	if track == nil {
		slog.WarnContext(ctx, "acquire_track_not_found", "track_id", trackId.String())
		return nil, nil
	}
	return track, nil
}

func (s *AcquireTrackAudioService) startAcquisition(ctx context.Context, userId shared.UserId, trackId shared.TrackId, track *ports.TrackSnapshot) *AcquisitionContext {
	jobReporterFrom(ctx).meta(track.Title, track.Artist, track.Album)

	slog.InfoContext(ctx, "track_acquisition_started",
		"track_id", trackId.String(),
		"user_id", userId.String(),
		"has_isrc", track.ISRC != nil,
	)
	s.events.Publish(ctx, userId, events.TypeTrackAcquisitionStarted, map[string]any{
		"track_id": trackId.String(),
	})
	return &AcquisitionContext{Track: buildTrackRef(track)}
}

func (s *AcquireTrackAudioService) runAcquisition(ctx context.Context, userId shared.UserId, trackId shared.TrackId, ac *AcquisitionContext) error {
	defer s.recordRejections(ctx, ac)
	identityStart := time.Now()
	s.resolveIdentity(ctx, ac)
	timeStage(ctx, "identity", identityStart)
	if err := RunPipeline(ctx, s.buildSteps(userId, trackId), ac); err != nil {
		return err
	}

	jobReporterFrom(ctx).fetched(ac.SourceFormat)
	jobReporterFrom(ctx).provenance(string(ac.Provenance()))
	s.onAcquireCompleted(ctx, userId, trackId, ac.AudioRef)
	return nil
}

func configureReplaceExclusion(ctx context.Context, ac *AcquisitionContext, track *ports.TrackSnapshot, trackId shared.TrackId) {
	ac.Replace.PreservedRef = deref(track.AudioRef)
	ac.Replace.ExcludeKeys = mergeSourceKeys(
		track.RejectedSourceKeys,
		sourceKey(deref(track.AudioSourceURL)),
	)
	if len(ac.Replace.ExcludeKeys) > 0 {
		slog.InfoContext(ctx, "acquisition.replacing_source",
			"track_id", trackId.String(), "excluded_keys", ac.Replace.ExcludeKeys)
	} else {
		ac.Replace.SkipTopRanked = true
		slog.InfoContext(ctx, "acquisition.replacing_unknown_source",
			"track_id", trackId.String())
	}
}

const settleBudget = 10 * time.Second

const supersededDeleteBudget = 30 * time.Second

func detachedTimeout(ctx context.Context, budget time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), budget)
}

func settleContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return detachedTimeout(ctx, settleBudget)
}

func (s *AcquireTrackAudioService) reportReplaceFailure(ctx context.Context, userId shared.UserId, trackId shared.TrackId, err error, ac *AcquisitionContext) error {
	slog.WarnContext(ctx, "track_acquisition_failed",
		"track_id", trackId.String(),
		"user_id", userId.String(),
		"replace", true,
		"error", redact.LogError(err),
	)
	reason := rejectionAwareReason(ctx, trackId, err, ac)
	settleCtx, cancel := settleContext(ctx)
	defer cancel()
	s.events.Publish(settleCtx, userId, events.TypeTrackReplaceFailed, map[string]any{
		"track_id": trackId.String(),
		"reason":   reason,
	})
	return err
}

func (s *AcquireTrackAudioService) reportSchedulerCancelledWithoutMarkingFailed(ctx context.Context, userId shared.UserId, trackId shared.TrackId, err error) error {
	slog.WarnContext(ctx, "track_acquisition_cancelled",
		"track_id", trackId.String(),
		"user_id", userId.String(),
		"error", redact.LogError(err),
	)
	return err
}

func willRetry(jobCtx context.Context, err error) bool {
	attempt := jobAttemptFrom(jobCtx)
	return attempt > 0 && attempt < maxAcquisitionAttempts && isTransientFailure(err)
}

func (s *AcquireTrackAudioService) reportRetryableFailure(ctx context.Context, userId shared.UserId, trackId shared.TrackId, err error) error {
	slog.WarnContext(ctx, "track_acquisition_retryable",
		"track_id", trackId.String(),
		"user_id", userId.String(),
		"error", redact.LogError(err),
	)
	return fmt.Errorf("%w: %w", ErrAcquisitionRetryable, err)
}

func (s *AcquireTrackAudioService) reportAcquireFailure(ctx context.Context, userId shared.UserId, trackId shared.TrackId, err error, ac *AcquisitionContext) error {
	slog.WarnContext(ctx, "track_acquisition_failed",
		"track_id", trackId.String(),
		"user_id", userId.String(),
		"replace", false,
		"error", redact.LogError(err),
	)
	reason := rejectionAwareReason(ctx, trackId, err, ac)
	settleCtx, cancel := settleContext(ctx)
	defer cancel()
	s.markFailed(settleCtx, trackId, userId, reason)
	s.events.Publish(settleCtx, userId, events.TypeTrackAcquisitionFailed, map[string]any{
		"track_id": trackId.String(),
		"reason":   reason,
	})
	return err
}

func rejectionAwareReason(ctx context.Context, trackId shared.TrackId, err error, ac *AcquisitionContext) string {
	summary := summarizeRejections(ac.Rejections)
	if summary == "" {
		return failureReason(err)
	}
	slog.InfoContext(ctx, "acquisition.rejection_summary",
		"track_id", trackId.String(), "summary", summary)
	return shared.JoinFailureReason(failureCode(err), summary)
}

func (s *AcquireTrackAudioService) reconcileForReacquire(ctx context.Context, track *ports.TrackSnapshot) (proceed bool, err error) {
	return reacquirePolicy{ledger: s.ledger, audioStore: s.audioStore}.reconcile(ctx, track)
}

func (s *AcquireTrackAudioService) onAcquireCompleted(ctx context.Context, userId shared.UserId, trackId shared.TrackId, audioRef string) {
	slog.InfoContext(ctx, "track_acquisition_completed",
		"track_id", trackId.String(),
		"user_id", userId.String(),
		"audio_ref", audioRef,
	)
	s.events.Publish(ctx, userId, events.TypeTrackAcquisitionCompleted, map[string]any{
		"track_id":  trackId.String(),
		"audio_ref": audioRef,
	})
}

func (s *AcquireTrackAudioService) markFailed(ctx context.Context, trackId shared.TrackId, userId shared.UserId, reason string) bool {
	err := s.ledger.Fail(ctx, trackId, userId, reason)
	if errors.Is(err, ports.ErrIllegalTransition) {
		slog.InfoContext(ctx, "mark_failed: track already settled, failure ignored",
			"track_id", trackId.String(), "error", redact.LogError(err))
		return false
	}
	if err != nil {
		slog.ErrorContext(ctx, "mark_failed: could not persist failure",
			"track_id", trackId.String(), "error", redact.LogError(err))
		return false
	}
	return true
}

func (s *AcquireTrackAudioService) RefuseQueued(ctx context.Context, userId shared.UserId, trackId shared.TrackId) {
	settleCtx, cancel := settleContext(ctx)
	defer cancel()
	if !s.markFailed(settleCtx, trackId, userId, string(shared.FailureAcquisitionRefused)) {
		return
	}
	s.events.Publish(settleCtx, userId, events.TypeTrackAcquisitionFailed, map[string]any{
		"track_id": trackId.String(),
		"reason":   string(shared.FailureAcquisitionRefused),
	})
}

func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

func buildTrackRef(track *ports.TrackSnapshot) TrackRef {
	return TrackRef{
		ID:          track.ID.String(),
		UserID:      track.UserId.String(),
		Title:       track.Title,
		Artist:      track.Artist,
		Album:       track.Album,
		Duration:    deref(track.DurationSeconds),
		ISRC:        deref(track.ISRC),
		Year:        deref(track.Year),
		TrackNumber: deref(track.TrackNumber),
		AlbumArtist: deref(track.AlbumArtist),
		Genre:       deref(track.Genre),

		CatalogDuration: deref(track.CatalogDurationSeconds),
		CatalogExplicit: track.CatalogExplicit,
	}
}
