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

// WithAudioProber enables post-download duration verification: each downloaded
// file is probed and rejected if its length doesn't match the track's expected
// duration, falling through to the next-best candidate.
func WithAudioProber(p ports.AudioProber) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.audioProber = p }
}

// WithAudioTagger enables metadata tagging of the downloaded file before it is
// stored. Without it the tag step is a no-op.
func WithAudioTagger(t ports.AudioTagger) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) { s.audioTagger = t }
}

// Execute acquires audio for a track via the search pipeline (YouTube-first,
// SoundCloud gap-fill). It always re-searches by metadata. The previous
// direct-download path (download a saved SoundCloud permalink verbatim) was
// removed: SoundCloud's public stream for many tracks is only a ~30s preview,
// which yt-dlp would store as if it were the full track. sourceURL is retained
// on the signature for the scheduler contract — and a future direct path gated
// by post-download duration validation — but is currently unused.
func (s *AcquireTrackAudioService) Execute(ctx context.Context, userId shared.UserId, trackId domain.TrackId, sourceURL string) error {
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
	// Server-authoritative "it's acquiring now" signal (F7/F8): the client seeds
	// its download UI from this and flips a re-acquired ready/failed track back
	// to pending, instead of depending on the optimistic save or the poll.
	if s.events != nil {
		s.events.Publish(userId, "track_acquisition_started", map[string]any{
			"track_id": trackId.String(),
		})
	}

	ac := &AcquisitionContext{Track: buildTrackRef(track)}
	err = RunPipeline(ctx, s.buildSteps(userId, trackId), ac)
	cleanupTemp(ctx, ac)

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

// failureReason maps an internal pipeline error to a short, stable, client-safe
// reason. The full error chain (which can carry yt-dlp stderr, file paths, and
// other internals) is logged at the call site and never stored on the track or
// returned over the wire. RunPipeline produces exactly two shapes: a *StepError
// for a failing step, or a "pipeline cancelled" error for context cancellation.
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

// reasonForStep maps a pipeline step name to its client-safe failure reason.
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

// reconcileForReacquire resets a non-pending track back to pending so the
// pipeline can run again, and reports whether acquisition should proceed. A
// ready track whose audio file still exists is a no-op skip (proceed=false); a
// ready track with a missing file, or a previously failed track, is reverted to
// pending (proceed=true). A fresh pending track proceeds unchanged.
func (s *AcquireTrackAudioService) reconcileForReacquire(ctx context.Context, track *domain.Track) (proceed bool, err error) {
	switch track.AcquisitionStatus {
	case domain.AcquisitionReady:
		if track.AudioRef != nil {
			exists, existsErr := s.audioStore.Exists(ctx, *track.AudioRef)
			switch {
			case existsErr != nil:
				// A transient existence-check error must not leave a possibly-missing
				// file unrepaired: fall through to re-acquire rather than skipping.
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
		track.RevertToPending()
		if err := s.trackRepo.Update(ctx, track); err != nil {
			return false, fmt.Errorf("revert to pending: %w", err)
		}
	case domain.AcquisitionFailed:
		slog.InfoContext(ctx, "acquire_retrying_failed", "track_id", track.ID.String())
		track.RevertToPending()
		if err := s.trackRepo.Update(ctx, track); err != nil {
			return false, fmt.Errorf("revert failed to pending: %w", err)
		}
	}
	return true, nil
}

// buildSteps assembles the acquisition pipeline: discover a candidate
// (search + select), then the shared download → tag → store → update-track tail.
func (s *AcquireTrackAudioService) buildSteps(userId shared.UserId, trackId domain.TrackId) []Step {
	return []Step{
		NewSearchStep(s.audioSearcher),
		NewSelectStep(),
		NewDownloadStep(s.audioSearcher, WithDownloadProber(s.audioProber)),
		NewTagStep(s.audioTagger),
		NewStoreStep(s.audioStore, WithStoreProber(s.audioProber)),
		NewUpdateTrackStep(s.trackRepo, userId, trackId),
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
	track, err := s.trackRepo.GetByID(ctx, trackId, userId)
	if err != nil {
		slog.ErrorContext(ctx, "mark_failed: get track failed",
			"track_id", trackId.String(), "error", err)
		return
	}
	if track == nil {
		return
	}
	if markErr := track.MarkFailed(reason); markErr != nil {
		slog.ErrorContext(ctx, "mark_failed: domain error",
			"track_id", trackId.String(), "error", markErr)
		return
	}
	if err := s.trackRepo.Update(ctx, track); err != nil {
		slog.ErrorContext(ctx, "mark_failed: persist failed, track stuck in pending",
			"track_id", trackId.String(), "error", err)
	}
}

// deref returns the pointee, or the zero value of T when p is nil.
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
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

func cleanupTemp(ctx context.Context, ac *AcquisitionContext) {
	if ac.TempPath == "" {
		return
	}
	parent := filepath.Dir(ac.TempPath)
	if err := os.RemoveAll(parent); err != nil {
		slog.WarnContext(ctx, "temp_cleanup_failed", "path", parent, "error", err)
	}
}
