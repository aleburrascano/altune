package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
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
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithAcquireEvents(pub events.Publisher) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.events = pub }
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

func (s *AcquireTrackAudioService) Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	return s.execute(ctx, userId, trackId, false)
}

func (s *AcquireTrackAudioService) ExecuteReplace(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	return s.execute(ctx, userId, trackId, true)
}

func (s *AcquireTrackAudioService) execute(
	ctx context.Context,
	userId shared.UserId,
	trackId domain.TrackId,
	replace bool,
) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil {
		return fmt.Errorf("get track: %w", err)
	}
	if track == nil {
		slog.WarnContext(ctx, "acquire_track_not_found", "track_id", trackId.String())
		return nil
	}

	if !replace {
		proceed, reconcileErr := s.reconcileForReacquire(ctx, track)
		if reconcileErr != nil {
			return reconcileErr
		}
		if !proceed {
			return nil
		}
	}

	jobReporterFrom(ctx).meta(track.Title, track.Artist, track.Album)

	slog.InfoContext(ctx, "track_acquisition_started",
		"track_id", trackId.String(),
		"user_id", userId.String(),
		"has_isrc", track.ISRC != nil,
	)
	if s.events != nil {
		s.events.Publish(userId, "track_acquisition_started", map[string]any{
			"track_id": trackId.String(),
		})
	}

	ac := &AcquisitionContext{Track: buildTrackRef(track)}
	if replace {
		ac.PreservedRef = deref(track.AudioRef)
		if current := deref(track.AudioSourceURL); current != "" {
			ac.ExcludeURLs = []string{current}
			slog.InfoContext(ctx, "acquisition.replacing_source",
				"track_id", trackId.String(), "excluded_source", current)
		}
	}
	s.resolveIdentity(ctx, ac)
	err = RunPipeline(ctx, s.buildSteps(userId, trackId), ac)
	CleanupTemp(ctx, ac)

	if err != nil {
		slog.WarnContext(ctx, "track_acquisition_failed",
			"track_id", trackId.String(),
			"user_id", userId.String(),
			"replace", replace,
			"error", err,
		)
		reason := failureReason(err)
		if !replace {
			s.markFailed(ctx, trackId, userId, reason)
		}
		if s.events != nil {
			s.events.Publish(userId, "track_acquisition_failed", map[string]any{
				"track_id": trackId.String(),
				"reason":   reason,
			})
		}
		return err
	}

	jobReporterFrom(ctx).provenance(string(ac.Provenance()))
	s.onAcquireCompleted(ctx, userId, trackId, ac.AudioRef)
	return nil
}

func failureReason(err error) string {
	var stepErr *StepError
	if errors.As(err, &stepErr) {
		if reason, ok := reasonForStep(stepErr.Step); ok {
			return reason
		}
		return "audio acquisition failed"
	}
	if strings.HasPrefix(err.Error(), "pipeline cancelled") {
		return "audio acquisition cancelled"
	}
	return "audio acquisition failed"
}

func reasonForStep(step string) (string, bool) {
	switch step {
	case "search", "select":
		return "no matching audio found", true
	case "download":
		return "audio download failed", true
	case "store":
		return "audio storage failed", true
	default:
		return "", false
	}
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
}

func (s *AcquireTrackAudioService) reconcileForReacquire(ctx context.Context, track *domain.Track) (proceed bool, err error) {
	switch track.AcquisitionStatus {
	case domain.AcquisitionReady:
		if track.AudioRef != nil {
			exists, existsErr := s.audioStore.Exists(ctx, *track.AudioRef)
			switch {
			case existsErr != nil:
				slog.WarnContext(ctx, "acquire_exists_check_failed",
					"track_id", track.ID.String(), "audio_ref", *track.AudioRef, "error", existsErr)
			case exists:
				slog.InfoContext(ctx, "acquire_skip_already_ready", "track_id", track.ID.String())
				return false, nil
			default:
				slog.InfoContext(ctx, "acquire_reacquire_missing_file",
					"track_id", track.ID.String(), "audio_ref", *track.AudioRef)
			}
		}
		if err := s.revertToPending(ctx, track); err != nil {
			return false, err
		}
	case domain.AcquisitionFailed:
		slog.InfoContext(ctx, "acquire_retrying_failed", "track_id", track.ID.String())
		if err := s.revertToPending(ctx, track); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (s *AcquireTrackAudioService) revertToPending(ctx context.Context, track *domain.Track) error {
	track.RevertToPending()
	if err := s.trackRepo.Update(ctx, track); err != nil {
		return fmt.Errorf("revert to pending: %w", err)
	}
	return nil
}

func (s *AcquireTrackAudioService) buildSteps(userId shared.UserId, trackId domain.TrackId) []Step {
	return append(
		CoreSteps(s.sources, s.audioTagger, s.audioStore, s.audioProber, s.identifier),
		NewUpdateTrackStep(s.trackRepo, userId, trackId),
	)
}

func CoreSteps(
	sources *SourceRegistry,
	tagger ports.AudioTagger,
	store ports.AudioWriter,
	prober ports.AudioProber,
	identifier ports.AudioIdentifier,
) []Step {
	return []Step{
		NewSearchStep(sources),
		NewSelectStep(),
		NewDownloadStep(sources, WithDownloadProber(prober), WithDownloadIdentifier(identifier)),
		NewTagStep(tagger),
		NewStoreStep(store, WithStoreProber(prober)),
	}
}

func (s *AcquireTrackAudioService) onAcquireCompleted(ctx context.Context, userId shared.UserId, trackId domain.TrackId, audioRef string) {
	slog.InfoContext(ctx, "track_acquisition_completed",
		"track_id", trackId.String(),
		"user_id", userId.String(),
		"audio_ref", audioRef,
	)
	if s.events != nil {
		s.events.Publish(userId, "track_acquisition_completed", map[string]any{
			"track_id":  trackId.String(),
			"audio_ref": audioRef,
		})
	}
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

func CleanupTemp(ctx context.Context, ac *AcquisitionContext) {
	if ac.TempPath == "" {
		return
	}
	parent := filepath.Dir(ac.TempPath)
	if err := os.RemoveAll(parent); err != nil {
		slog.WarnContext(ctx, "temp_cleanup_failed", "path", parent, "error", err)
	}
}
