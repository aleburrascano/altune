package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	catalogports "altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type AcquireTrackAudioService struct {
	trackRepo   ports.TrackRepository
	sources     *SourceRegistry
	audioStore  ports.AudioWriter
	audioProber ports.AudioProber
	audioTagger ports.AudioTagger
	identifier  ports.AudioIdentifier
	recordings  ports.RecordingResolver
	events      events.Publisher
}

func NewAcquireTrackAudioService(
	trackRepo ports.TrackRepository,
	sources *SourceRegistry,
	audioStore ports.AudioWriter,
	opts ...func(*AcquireTrackAudioService),
) *AcquireTrackAudioService {
	s := &AcquireTrackAudioService{
		trackRepo:  trackRepo,
		sources:    sources,
		audioStore: audioStore,
		recordings: ports.NoopRecordingResolver(),
		events:     events.NoopPublisher(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithAcquireEvents(pub events.Publisher) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) {
		if pub != nil {
			s.events = pub
		}
	}
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

const acquireTimeout = 10 * time.Minute

// Execute acquires audio for a track, first reconciling any existing audio so
// a track that already has a valid file is not re-acquired.
func (s *AcquireTrackAudioService) Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
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
	// Guard temp cleanup with defer so a panic anywhere in the pipeline (or in
	// acquire.go itself) still removes the downloaded temp dir on the way out.
	defer CleanupTemp(ctx, ac)
	if err := s.runAcquisition(ctx, userId, trackId, ac); err != nil {
		return s.reportAcquireFailure(ctx, userId, trackId, err, ac)
	}
	return nil
}

// ExecuteReplace acquires a different source for a track that already has
// audio, excluding the current and previously rejected sources. A failed
// replace leaves the track's existing audio and status untouched.
func (s *AcquireTrackAudioService) ExecuteReplace(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	ctx, cancel := context.WithTimeout(ctx, acquireTimeout)
	defer cancel()

	track, err := s.loadTrack(ctx, userId, trackId)
	if err != nil || track == nil {
		return err
	}

	ac := s.startAcquisition(ctx, userId, trackId, track)
	// Guard temp cleanup with defer so a panic anywhere in the pipeline (or in
	// acquire.go itself) still removes the downloaded temp dir on the way out.
	defer CleanupTemp(ctx, ac)
	configureReplaceExclusion(ctx, ac, track, trackId)
	if err := s.runAcquisition(ctx, userId, trackId, ac); err != nil {
		return s.reportReplaceFailure(ctx, userId, trackId, err, ac)
	}
	s.deleteSupersededAudio(ctx, trackId, ac)
	return nil
}

// deleteSupersededAudio removes the audio a successful replace swapped out. It
// runs only after update_track committed the new ref, so any earlier failure
// leaves the original object serving. The swap is already durable, so the
// delete gets its own budget past the job deadline, and a delete error only
// orphans the old object: it is logged, not returned.
func (s *AcquireTrackAudioService) deleteSupersededAudio(ctx context.Context, trackId domain.TrackId, ac *AcquisitionContext) {
	old := ac.Replace.PreservedRef
	if old == "" || old == ac.AudioRef {
		return
	}
	delCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if s.servedByAnotherTrack(delCtx, trackId, old) {
		return
	}
	if err := s.audioStore.Delete(delCtx, old); err != nil {
		slog.ErrorContext(ctx, "acquisition.replace_orphaned_old_audio",
			"track_id", trackId.String(), "audio_ref", old, "error", logSafeError(err))
	}
}

// servedByAnotherTrack reports whether audioRef is some other track's audio —
// canonical refs are shared by tracks with equivalent metadata (#1984). An
// unanswerable check counts as shared: keeping the object orphans it for the
// reconcile sweep, deleting it strips a Ready track of its file.
func (s *AcquireTrackAudioService) servedByAnotherTrack(ctx context.Context, trackId domain.TrackId, audioRef string) bool {
	inUse, err := s.trackRepo.AudioRefInUse(ctx, audioRef, trackId)
	if err != nil {
		slog.ErrorContext(ctx, "acquisition.replace_audio_usage_unknown",
			"track_id", trackId.String(), "audio_ref", audioRef, "error", logSafeError(err))
		return true
	}
	if inUse {
		slog.InfoContext(ctx, "acquisition.replace_kept_shared_audio",
			"track_id", trackId.String(), "audio_ref", audioRef)
	}
	return inUse
}

// loadTrack returns (nil, nil) when the track does not exist, which both
// entry points treat as nothing to do.
func (s *AcquireTrackAudioService) loadTrack(ctx context.Context, userId shared.UserId, trackId domain.TrackId) (*domain.Track, error) {
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil {
		return nil, fmt.Errorf("get track: %w", err)
	}
	if track == nil {
		slog.WarnContext(ctx, "acquire_track_not_found", "track_id", trackId.String())
		return nil, nil
	}
	return track, nil
}

// startAcquisition announces the acquisition and returns its pipeline context.
// The caller owns deferring CleanupTemp on the returned context.
func (s *AcquireTrackAudioService) startAcquisition(ctx context.Context, userId shared.UserId, trackId domain.TrackId, track *domain.Track) *AcquisitionContext {
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

// runAcquisition runs the pipeline and reports success; a pipeline error is
// returned unreported so the caller can apply its own failure policy.
func (s *AcquireTrackAudioService) runAcquisition(ctx context.Context, userId shared.UserId, trackId domain.TrackId, ac *AcquisitionContext) error {
	s.resolveIdentity(ctx, ac)
	if err := RunPipeline(ctx, s.buildSteps(userId, trackId), ac); err != nil {
		return err
	}

	jobReporterFrom(ctx).provenance(string(ac.Provenance()))
	s.onAcquireCompleted(ctx, userId, trackId, ac.AudioRef)
	return nil
}

func configureReplaceExclusion(ctx context.Context, ac *AcquisitionContext, track *domain.Track, trackId domain.TrackId) {
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

// settleBudget is how long recording a failure gets once the job's own budget
// is gone.
const settleBudget = 10 * time.Second

// settleContext detaches from ctx's cancellation for the failure settle. The
// settle runs precisely when ctx is most likely already done — acquireTimeout
// fired, or Shutdown cancelled the scheduler's base context — and a settle on a
// dead context records nothing: the track stays pending until the stale sweep
// ten minutes later, spinning in the user's library on every deploy (#1975).
// ctx's values are kept so the write and its event stay correlated to the job.
func settleContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), settleBudget)
}

// reportReplaceFailure publishes track_replace_failed and returns err. The
// track is not marked failed: its existing audio is still valid.
func (s *AcquireTrackAudioService) reportReplaceFailure(ctx context.Context, userId shared.UserId, trackId domain.TrackId, err error, ac *AcquisitionContext) error {
	slog.WarnContext(ctx, "track_acquisition_failed",
		"track_id", trackId.String(),
		"user_id", userId.String(),
		"replace", true,
		"error", logSafeError(err),
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

// reportAcquireFailure marks the track failed, publishes
// track_acquisition_failed, and returns err.
func (s *AcquireTrackAudioService) reportAcquireFailure(ctx context.Context, userId shared.UserId, trackId domain.TrackId, err error, ac *AcquisitionContext) error {
	slog.WarnContext(ctx, "track_acquisition_failed",
		"track_id", trackId.String(),
		"user_id", userId.String(),
		"replace", false,
		"error", logSafeError(err),
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

// rejectionAwareReason is the user-facing failure reason, suffixed with a
// summary of rejected candidates when there were any.
func rejectionAwareReason(ctx context.Context, trackId domain.TrackId, err error, ac *AcquisitionContext) string {
	reason := failureReason(err)
	summary := summarizeRejections(ac.Rejections)
	if summary == "" {
		return reason
	}
	slog.InfoContext(ctx, "acquisition.rejection_summary",
		"track_id", trackId.String(), "summary", summary)
	return reason + domain.FailureDetailSeparator + summary
}

func (s *AcquireTrackAudioService) resolveIdentity(ctx context.Context, ac *AcquisitionContext) {
	identity, err := s.recordings.Resolve(ctx, ports.RecordingQuery{
		Title:  ac.Track.Title,
		Artist: ac.Track.Artist,
		Album:  ac.Track.Album,
		ISRC:   ac.Track.ISRC,
	})
	if err != nil {
		slog.WarnContext(ctx, "acquisition.identity_resolve_failed",
			"track_id", ac.Track.ID, "error", logSafeError(err))
		return
	}
	if identity.IsZero() {
		return
	}

	ac.Identity = identity
	if ac.Track.Duration <= 0 && identity.Duration > 0 {
		ac.Track.Duration = identity.Duration
		slog.InfoContext(ctx, "acquisition.identity_supplied_duration",
			"track_id", ac.Track.ID, "duration", identity.Duration)
	}
	if ac.Track.ISRC == "" && identity.ISRC != "" {
		ac.Track.ISRC = identity.ISRC
	}
	s.resolveExpectedCluster(ctx, ac)
}

func (s *AcquireTrackAudioService) resolveExpectedCluster(ctx context.Context, ac *AcquisitionContext) {
	if s.identifier == nil || ac.Identity.MBID == "" {
		return
	}

	cluster, err := s.identifier.AcoustIDsFor(ctx, ac.Identity.MBID)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.expected_cluster_failed",
			"track_id", ac.Track.ID, "mbid", ac.Identity.MBID, "error", logSafeError(err))
		return
	}
	if len(cluster) == 0 {
		slog.InfoContext(ctx, "acquisition.expected_cluster_unknown",
			"track_id", ac.Track.ID, "mbid", ac.Identity.MBID)
		return
	}

	ac.Identity.AcoustIDs = cluster
	slog.InfoContext(ctx, "acquisition.expected_cluster_resolved",
		"track_id", ac.Track.ID, "mbid", ac.Identity.MBID, "acoustids", len(cluster))
}

func (s *AcquireTrackAudioService) reconcileForReacquire(ctx context.Context, track *domain.Track) (proceed bool, err error) {
	return reacquirePolicy{trackRepo: s.trackRepo, audioStore: s.audioStore}.reconcile(ctx, track)
}

func (s *AcquireTrackAudioService) onAcquireCompleted(ctx context.Context, userId shared.UserId, trackId domain.TrackId, audioRef string) {
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

func (s *AcquireTrackAudioService) markFailed(ctx context.Context, trackId domain.TrackId, userId shared.UserId, reason string) {
	err := loadAndUpdate(ctx, s.trackRepo, trackId, userId, nil, func(track *domain.Track) error {
		return track.FailAcquisition(reason)
	})
	if errors.Is(err, domain.ErrIllegalAcquisitionTransition) {
		// Another path already settled the track (a concurrent success, the
		// stale-pending sweep): this failure is stale and must not overwrite it.
		slog.InfoContext(ctx, "mark_failed: track already settled, failure ignored",
			"track_id", trackId.String(), "error", logSafeError(err))
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "mark_failed: could not persist failure",
			"track_id", trackId.String(), "error", logSafeError(err))
	}
}

func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// loadAndUpdateMaxAttempts bounds the CAS retry in loadAndUpdate so a
// pathological stream of concurrent writers cannot spin it forever. Real settle
// contention on one track is tiny (a racing settle, the stale-pending sweeper),
// so a handful of attempts converges; the bound is a live-lock guard, not a
// tuned value.
const loadAndUpdateMaxAttempts = 5

// loadAndUpdate is the read-modify-write behind every acquisition settle: read
// the owned track, drive a state change on it, write it back under the
// optimistic-lock CAS at the version that was read (#1419).
//
// A CAS miss (catalogports.ErrTrackVersionConflict) means a concurrent writer —
// a racing settle, or the stale-pending sweeper — advanced the row between the
// read and the write, so the snapshot mutate ran against is stale. Rather than
// clobber the winner, it reloads and re-applies: mutate re-runs on the fresh
// state, so a mutate that guards on state (MarkReady, MarkFailed,
// RevertToPending) surfaces its own "already settled" error when the winner has
// reached a terminal state — the intended resolution of the sweeper-vs-settle
// race. A non-conflict error, or exhausting the bounded attempts, is returned.
func loadAndUpdate(ctx context.Context, repo ports.TrackRepository, id domain.TrackId, userId shared.UserId, notFound error, mutate func(*domain.Track) error) error {
	var lastErr error
	for attempt := 0; attempt < loadAndUpdateMaxAttempts; attempt++ {
		track, err := repo.GetByID(ctx, id, userId)
		if err != nil {
			return fmt.Errorf("get track: %w", err)
		}
		if track == nil {
			return notFound
		}
		expectedVersion := track.Version
		if err := mutate(track); err != nil {
			return err
		}
		lastErr = repo.Update(ctx, track, expectedVersion)
		if lastErr == nil {
			return nil
		}
		if !errors.Is(lastErr, catalogports.ErrTrackVersionConflict) {
			return fmt.Errorf("update track: %w", lastErr)
		}
	}
	return fmt.Errorf("update track: exhausted %d CAS attempts: %w", loadAndUpdateMaxAttempts, lastErr)
}

func buildTrackRef(track *domain.Track) TrackRef {
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
	}
}
