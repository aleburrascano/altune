package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
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
	return nil
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
	s.events.Publish(userId, "track_acquisition_started", map[string]any{
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
	s.events.Publish(userId, "track_replace_failed", map[string]any{
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
	s.markFailed(ctx, trackId, userId, reason)
	s.events.Publish(userId, "track_acquisition_failed", map[string]any{
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
	return reason + ": " + summary
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
			"track_id", ac.Track.ID, "error", err)
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
			"track_id", ac.Track.ID, "mbid", ac.Identity.MBID, "error", err)
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
	s.events.Publish(userId, "track_acquisition_completed", map[string]any{
		"track_id":  trackId.String(),
		"audio_ref": audioRef,
	})
}

func (s *AcquireTrackAudioService) markFailed(ctx context.Context, trackId domain.TrackId, userId shared.UserId, reason string) {
	err := loadAndUpdate(ctx, s.trackRepo, trackId, userId, nil, func(track *domain.Track) error {
		return track.MarkFailed(reason)
	})
	if err != nil {
		slog.ErrorContext(ctx, "mark_failed: could not persist failure",
			"track_id", trackId.String(), "error", err)
	}
}

func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

func loadAndUpdate(ctx context.Context, repo ports.TrackRepository, id domain.TrackId, userId shared.UserId, notFound error, mutate func(*domain.Track) error) error {
	track, err := repo.GetByID(ctx, id, userId)
	if err != nil {
		return fmt.Errorf("get track: %w", err)
	}
	if track == nil {
		return notFound
	}
	if err := mutate(track); err != nil {
		return err
	}
	if err := repo.Update(ctx, track); err != nil {
		return fmt.Errorf("update track: %w", err)
	}
	return nil
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
