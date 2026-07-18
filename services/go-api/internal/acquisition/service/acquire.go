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

// revertToPending persists an already-loaded track back to AcquisitionPending.
func (s *AcquireTrackAudioService) revertToPending(ctx context.Context, track *domain.Track) error {
	track.RevertToPending()
	if err := s.trackRepo.Update(ctx, track); err != nil {
		return fmt.Errorf("revert to pending: %w", err)
	}
	return nil
}

// buildSteps assembles the acquisition pipeline: discover a candidate
// (search + select), then the shared download → tag → store → update-track tail.
func (s *AcquireTrackAudioService) buildSteps(userId shared.UserId, trackId domain.TrackId) []Step {
	return append(
		CoreSteps(s.audioSearcher, s.audioTagger, s.audioStore, s.audioProber),
		NewUpdateTrackStep(s.trackRepo, userId, trackId),
	)
}

// CoreSteps assembles the search → select → download → tag → store sequence
// shared by every caller that replays the acquisition pipeline: the production
// service (via buildSteps, plus its own UpdateTrackStep) and the reacquire CLI
// commands, which update audio_ref themselves and so stop before that step.
// One place decides the core pipeline's shape so the two callers cannot drift.
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

// deref returns the pointee, or the zero value of T when p is nil.
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// loadAndUpdate fetches a track, applies mutate, and persists the result.
// notFound is returned (nil to swallow) when the track no longer exists.
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

// CleanupTemp removes the temp directory a downloaded file lived in (the
// parent of TempPath, not TempPath itself — DownloadStep creates one temp dir
// per download attempt via os.MkdirTemp). Exported so the reacquire CLI
// commands, which replay the pipeline outside Execute, clean up the same way
// instead of re-deriving the convention.
func CleanupTemp(ctx context.Context, ac *AcquisitionContext) {
	if ac.TempPath == "" {
		return
	}
	parent := filepath.Dir(ac.TempPath)
	if err := os.RemoveAll(parent); err != nil {
		slog.WarnContext(ctx, "temp_cleanup_failed", "path", parent, "error", err)
	}
}
