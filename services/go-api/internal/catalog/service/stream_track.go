package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
	"log/slog"
	"time"
)

type StreamOutput struct {
	Reader ports.AudioStream
	Size   int64
	Track  *domain.Track
}

type StreamTrackService struct {
	trackRepo  ports.TrackReadWriter
	audioStore ports.AudioStore
	scheduler  ports.AcquisitionScheduler
	metrics    ports.AudioStoreMetrics
}

func NewStreamTrackService(
	trackRepo ports.TrackReadWriter,
	audioStore ports.AudioStore,
	opts ...func(*StreamTrackService),
) *StreamTrackService {
	s := &StreamTrackService{
		trackRepo:  trackRepo,
		audioStore: audioStore,
		scheduler:  ports.NoopAcquisitionScheduler(),
		metrics:    ports.NoopAudioStoreMetrics(),
	}
	return applyOptions(s, opts)
}

func WithStreamScheduler(scheduler ports.AcquisitionScheduler) func(*StreamTrackService) {
	return func(s *StreamTrackService) {
		if scheduler != nil {
			s.scheduler = scheduler
		}
	}
}

func WithStreamMetrics(m ports.AudioStoreMetrics) func(*StreamTrackService) {
	return func(s *StreamTrackService) {
		if m != nil {
			s.metrics = m
		}
	}
}

func (s *StreamTrackService) Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId) (*StreamOutput, error) {
	dbStart := time.Now()
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	dbDuration := time.Since(dbStart)
	if err != nil {
		return nil, fmt.Errorf("stream track: %w", err)
	}
	if track == nil {
		return nil, ErrTrackNotFound
	}

	if !track.IsStreamable() {
		return nil, ErrAudioNotAvailable
	}

	storageStart := time.Now()
	reader, size, err := s.audioStore.Stream(ctx, *track.AudioRef)
	if err != nil {
		s.metrics.StreamRecoveryTriggered()
		slog.WarnContext(ctx, "stream.audio_missing",
			"track_id", trackId.String(), "error", err)
		confirmedMissing, recErr := s.recoverMissingAudio(ctx, userId, track)
		if recErr != nil {
			slog.ErrorContext(ctx, "stream.recover_failed",
				"track_id", trackId.String(), "error", recErr)
		}
		// Only a confirmed absence is genuinely "not available"; a transient
		// stream failure over a present (or unverifiable) file is retryable.
		if confirmedMissing {
			return nil, ErrAudioNotAvailable
		}
		return nil, ErrAudioTemporarilyUnavailable
	}

	slog.InfoContext(ctx, "stream.opened",
		"track_id", trackId.String(),
		"db_lookup_ms", dbDuration.Milliseconds(),
		"storage_open_ms", time.Since(storageStart).Milliseconds(),
	)

	return &StreamOutput{Reader: reader, Size: size, Track: track}, nil
}

func (s *StreamTrackService) RecoverIfMissing(ctx context.Context, userId shared.UserId, trackId domain.TrackId) error {
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil {
		return fmt.Errorf("recover audio: %w", err)
	}
	if track == nil || !track.IsStreamable() {
		return nil
	}

	exists, err := s.audioStore.Exists(ctx, *track.AudioRef)
	if err != nil {
		return fmt.Errorf("recover audio: exists check: %w", err)
	}
	if exists {
		return nil
	}
	return s.reconcileMissingAudio(ctx, userId, track, false, nil)
}

// recoverMissingAudio reconciles a failed stream against storage and reports
// whether the file was confirmed absent. confirmedMissing is true only when the
// existence check succeeded and the file is genuinely gone; a failed existence
// check or a present file both leave it false (the stream failure is transient).
func (s *StreamTrackService) recoverMissingAudio(ctx context.Context, userId shared.UserId, track *domain.Track) (confirmedMissing bool, err error) {
	exists, existsErr := s.audioStore.Exists(ctx, *track.AudioRef)
	recErr := s.reconcileMissingAudio(ctx, userId, track, exists, existsErr)
	return existsErr == nil && !exists, recErr
}

// reconcileMissingAudio acts on an existence-check verdict. A failed check or a
// present file is a no-op; a missing file is marked failed and persisted, and
// only once that persist succeeds is re-acquisition scheduled. Scheduling over
// an unpersisted row would race a new job against the stale stored state; the
// row still points at the missing file, so the next stream retries recovery.
func (s *StreamTrackService) reconcileMissingAudio(ctx context.Context, userId shared.UserId, track *domain.Track, exists bool, err error) error {
	if err != nil {
		return fmt.Errorf("audio existence check: %w", err)
	}
	if exists {
		return nil
	}
	if err := s.markAudioMissing(ctx, userId, track); err != nil {
		return err
	}
	s.scheduleReacquire(ctx, userId, track.ID)
	return nil
}

// markAudioMissing marks track failed for missing audio and persists it.
func (s *StreamTrackService) markAudioMissing(ctx context.Context, userId shared.UserId, track *domain.Track) error {
	if err := track.MarkFailed("audio file missing from storage"); err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	slog.WarnContext(ctx, "track marked failed: audio file missing",
		"track_id", track.ID.String(), "user_id", userId.String())
	if err := s.trackRepo.Update(ctx, track); err != nil {
		return fmt.Errorf("persist recovery: %w", err)
	}
	return nil
}

// scheduleReacquire queues re-acquisition of trackId. A refusal is logged, not
// returned: the track was just marked failed, so the retry path can reclaim it.
func (s *StreamTrackService) scheduleReacquire(ctx context.Context, userId shared.UserId, trackId domain.TrackId) {
	slog.InfoContext(ctx, "stream.reacquire_scheduled",
		"track_id", trackId.String())
	if err := s.scheduler.Schedule(ctx, userId, trackId, ""); err != nil {
		slog.WarnContext(ctx, "stream.reacquire_refused",
			"track_id", trackId.String(), "error", err)
	}
}
