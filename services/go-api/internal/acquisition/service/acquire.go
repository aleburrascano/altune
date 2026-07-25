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
	trackRepo     ports.TrackRepository
	audioSearcher ports.AudioSearcher
	audioStore    ports.AudioWriter
	audioProber   ports.AudioProber
	audioTagger   ports.AudioTagger
	events        events.Publisher
}

func NewAcquireTrackAudioService(
	trackRepo ports.TrackRepository,
	audioSearcher ports.AudioSearcher,
	audioStore ports.AudioWriter,
	opts ...func(*AcquireTrackAudioService),
) *AcquireTrackAudioService {
	s := &AcquireTrackAudioService{
		trackRepo:     trackRepo,
		audioSearcher: audioSearcher,
		audioStore:    audioStore,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithAcquireEvents(pub events.Publisher) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.events = pub }
}

func WithAudioProber(p ports.AudioProber) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.audioProber = p }
}

func WithAudioTagger(t ports.AudioTagger) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.audioTagger = t }
}

func (s *AcquireTrackAudioService) Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
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

	proceed, err := s.reconcileForReacquire(ctx, track)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
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
	err = RunPipeline(ctx, s.buildSteps(userId, trackId), ac)
	CleanupTemp(ctx, ac)

	if err != nil {
		slog.WarnContext(ctx, "track_acquisition_failed",
			"track_id", trackId.String(),
			"user_id", userId.String(),
			"error", err,
		)
		reason := failureReason(err)
		s.markFailed(ctx, trackId, userId, reason)
		if s.events != nil {
			s.events.Publish(userId, "track_acquisition_failed", map[string]any{
				"track_id": trackId.String(),
				"reason":   reason,
			})
		}
		return err
	}

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
		CoreSteps(s.audioSearcher, s.audioTagger, s.audioStore, s.audioProber),
		NewUpdateTrackStep(s.trackRepo, userId, trackId),
	)
}

func CoreSteps(searcher ports.AudioSearcher, tagger ports.AudioTagger, store ports.AudioWriter, prober ports.AudioProber) []Step {
	return []Step{
		NewSearchStep(searcher),
		NewSelectStep(),
		NewDownloadStep(searcher, WithDownloadProber(prober)),
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
