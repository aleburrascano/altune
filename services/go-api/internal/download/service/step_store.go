package service

import (
	"altune/go-api/internal/download/internal/core/audiokey"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/backoff"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
)

const (
	rollbackDeleteTries = 3
)

var rollbackDeleteBackoff = backoff.MustCapped(100*time.Millisecond, time.Second)

type StoreStep struct {
	audioStore  ports.AudioWriter
	prober      ports.AudioProber
	trackRefs   ports.AudioRefLookup
	ownTrackID  ids.TrackId
	attemptID   func() string
	orphans     ports.OrphanedAudioRecorder
	userID      ids.UserId
	keyPrefix   string
	deleteTries int
	workspace   ports.ScratchFolder
}

func NewStoreStep(audioStore ports.AudioWriter, workspace ports.ScratchFolder, prober ports.AudioProber, orphans ports.OrphanedAudioRecorder, refs ports.AudioRefLookup, opts ...func(*StoreStep)) (*StoreStep, error) {
	const owner = "NewStoreStep"
	if err := errors.Join(
		missingPort(owner, "prober", prober == nil),
		missingPort(owner, "orphan recorder", orphans == nil),
		missingPort(owner, "audio ref lookup", refs == nil),
		requireWorkspace(owner, workspace),
	); err != nil {
		return nil, err
	}
	return buildStoreStep(audioStore, workspace, prober, orphans, refs, opts...), nil
}

func buildStoreStep(audioStore ports.AudioWriter, workspace ports.ScratchFolder, prober ports.AudioProber, orphans ports.OrphanedAudioRecorder, refs ports.AudioRefLookup, opts ...func(*StoreStep)) *StoreStep {
	s := &StoreStep{
		audioStore:  audioStore,
		workspace:   workspace,
		prober:      prober,
		orphans:     orphans,
		trackRefs:   refs,
		attemptID:   ids.RandomID,
		deleteTries: rollbackDeleteTries,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithStoreOwner(userID ids.UserId, ownTrackID ids.TrackId) func(*StoreStep) {
	return func(s *StoreStep) {
		s.userID = userID
		s.ownTrackID = ownTrackID
	}
}

func WithStoreKeyPrefix(prefix string) func(*StoreStep) {
	return func(s *StoreStep) { s.keyPrefix = prefix }
}

func (s *StoreStep) Name() StepName { return stepNameStore }

func (s *StoreStep) Execute(ctx context.Context, ac *AcquisitionContext, tagged afterTag) (AfterStore, error) {
	if err := s.prober.ValidateDecodable(ctx, ac.TempPath); err != nil {
		return AfterStore{}, withCancellation(ctx, fmt.Errorf("final audio failed decode validation: %w", err))
	}

	audioRef := s.refFor(ctx, ac)
	ac.AudioRef = audioRef

	file := s.storedFileAt(ac.TempPath)
	if err := s.audioStore.Store(ctx, ac.TempPath, audioRef); err != nil {
		return AfterStore{}, withCancellation(ctx, fmt.Errorf("store audio: %w", err))
	}
	jobReporterFrom(ctx).stored(file)

	return AfterStore(tagged), nil
}

func (s *StoreStep) refFor(ctx context.Context, ac *AcquisitionContext) string {
	audioRef := s.keyPrefix + BuildAudioRef(ac.Track, ac.TempPath)
	if ac.Replace.PreservedRef != "" {
		return audiokey.StagedReplace(audioRef, s.attemptID())
	}
	if s.sharedWithAnotherTrack(ctx, audioRef) {
		return audiokey.TrackScoped(audioRef, s.ownTrackID)
	}
	return audioRef
}

func (s *StoreStep) storedFileAt(path string) ports.StoredFile {
	file := ports.StoredFile{Codec: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")}
	if size, err := s.workspace.Size(path); err == nil {
		file.Bytes = size
	}
	return file
}

func (s *StoreStep) Rollback(ctx context.Context, ac *AcquisitionContext) error {
	if rollbackHandedOver(ctx) {
		recordOrphanedAudio(ctx, s.orphans, s.userID, s.ownTrackID, ac.AudioRef)
		return nil
	}
	if s.stillServesATrack(ctx, ac) {
		return nil
	}
	err := s.deleteWithRetry(ctx, ac.AudioRef)
	if err != nil {
		recordOrphanedAudio(ctx, s.orphans, s.userID, s.ownTrackID, ac.AudioRef)
	}
	return err
}

func (s *StoreStep) stillServesATrack(ctx context.Context, ac *AcquisitionContext) bool {
	if !s.sharedWithAnotherTrack(ctx, ac.AudioRef) {
		return false
	}
	slog.WarnContext(ctx, "acquisition.rollback_kept_shared_audio",
		"audio_ref", ac.AudioRef, "track_id", ac.Track.ID)
	return true
}

func (s *StoreStep) sharedWithAnotherTrack(ctx context.Context, audioRef string) bool {
	inUse, err := s.trackRefs.AudioRefInUse(ctx, audioRef, s.ownTrackID)
	if err != nil {
		slog.ErrorContext(ctx, "acquisition.audio_usage_unknown",
			"audio_ref", audioRef, "error", redact.LogError(err))
		return true
	}
	return inUse
}

func (s *StoreStep) deleteWithRetry(ctx context.Context, audioRef string) error {
	var err error
	for attempt := 1; attempt <= s.deleteTries; attempt++ {
		if err = s.audioStore.Delete(ctx, audioRef); err == nil {
			return nil
		}
		slog.WarnContext(ctx, "acquisition.rollback_delete_retrying",
			"audio_ref", audioRef, "attempt", attempt, "error", err)
		if ctx.Err() != nil {
			break
		}
		if attempt < s.deleteTries {
			time.Sleep(rollbackDeleteBackoff.Delay(attempt))
		}
	}
	slog.ErrorContext(ctx, "orphaned audio file after rollback",
		"audio_ref", audioRef, "error", err)
	return fmt.Errorf("rollback delete audio %q: %w", audioRef, err)
}
