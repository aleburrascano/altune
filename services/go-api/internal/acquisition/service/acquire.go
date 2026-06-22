package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
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

// Execute acquires audio for a track. When sourceURL is a directly-downloadable
// source (a SoundCloud link — the only discovery provider that is also a download
// source), it downloads that exact track instead of re-searching by metadata; on
// any failure it falls back to the search pipeline. sourceURL is empty for
// retries and stream-triggered re-acquisition, which always use search.
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

	slog.InfoContext(ctx, "track_acquisition_started",
		"track_id", trackId.String(),
		"user_id", userId.String(),
		"has_isrc", track.ISRC != nil,
	)

	// Direct path: the saved result carries the exact SoundCloud URL the user
	// discovered, so download that exact track instead of re-searching by metadata
	// (which can grab a wrong reupload). On any failure it falls back to search.
	if s.tryDirectAcquire(ctx, userId, trackId, track, sourceURL) {
		return nil
	}

	ac := &AcquisitionContext{Track: buildTrackRef(track)}
	err = RunPipeline(ctx, s.buildSteps(userId, trackId, false), ac)
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

// tryDirectAcquire attempts the direct-download path for a directly-downloadable
// source URL, skipping search. It reports whether acquisition completed; on any
// failure it returns false so the caller falls back to the search pipeline.
func (s *AcquireTrackAudioService) tryDirectAcquire(
	ctx context.Context,
	userId shared.UserId,
	trackId domain.TrackId,
	track *domain.Track,
	sourceURL string,
) bool {
	if !isDirectAcquireURL(sourceURL) {
		return false
	}
	direct := &AcquisitionContext{
		Track: buildTrackRef(track),
		Selected: &Candidate{
			URL:      sourceURL,
			Title:    track.Title,
			Artist:   track.Artist,
			Duration: derefFloat(track.DurationSeconds),
		},
	}
	err := RunPipeline(ctx, s.buildSteps(userId, trackId, true), direct)
	cleanupTemp(ctx, direct)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.direct_failed_falling_back",
			"track_id", trackId.String(), "url", sourceURL, "error", err)
		return false
	}
	slog.InfoContext(ctx, "acquisition.direct_source_used",
		"track_id", trackId.String(), "url", sourceURL)
	s.onAcquireCompleted(ctx, userId, trackId, direct.AudioRef)
	return true
}

// failureReason maps an internal pipeline error to a short, stable, client-safe
// reason. The full error chain (which can carry yt-dlp stderr, file paths, and
// other internals) is logged at the call site and never stored on the track or
// returned over the wire. Keys off RunPipeline's stable "step <name>: ..." and
// "pipeline cancelled: ..." wrapping.
func failureReason(err error) string {
	// Preferred path: map on the structured step name, robust to message changes.
	var stepErr *StepError
	if errors.As(err, &stepErr) {
		if reason, ok := reasonForStep(stepErr.Step); ok {
			return reason
		}
		return "audio acquisition failed"
	}

	// Fallback: a plain "step <name>: ..." / "pipeline cancelled: ..." string
	// (e.g. errors produced outside RunPipeline). Kept for compatibility.
	msg := err.Error()
	switch {
	case strings.HasPrefix(msg, "step search:"), strings.HasPrefix(msg, "step select:"):
		return "no matching audio found"
	case strings.HasPrefix(msg, "step download:"):
		return "audio download failed"
	case strings.HasPrefix(msg, "step store:"):
		return "audio storage failed"
	case strings.HasPrefix(msg, "pipeline cancelled"):
		return "audio acquisition cancelled"
	default:
		return "audio acquisition failed"
	}
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

// buildSteps assembles the acquisition pipeline. The direct path pre-seeds a
// known downloadable source as Selected, so it skips Search+Select and goes
// straight to download; the search path discovers a candidate first. Both share
// the download → tag → store → update-track tail.
func (s *AcquireTrackAudioService) buildSteps(userId shared.UserId, trackId domain.TrackId, direct bool) []Step {
	var steps []Step
	if !direct {
		steps = append(steps, NewSearchStep(s.audioSearcher), NewSelectStep())
	}
	return append(steps,
		NewDownloadStep(s.audioSearcher),
		NewTagStep(),
		NewStoreStep(s.audioStore),
		NewUpdateTrackStep(s.trackRepo, userId, trackId),
	)
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

// isDirectAcquireURL reports whether the URL is one acquisition can download
// directly (skipping metadata search). Only SoundCloud qualifies today: among the
// discovery providers it is the only one that is both a search source and a
// yt-dlp-downloadable audio source (Deezer/iTunes/MusicBrainz are DRM/metadata).
func isDirectAcquireURL(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host != "soundcloud.com" && !strings.HasSuffix(host, ".soundcloud.com") {
		return false
	}
	// A SoundCloud set/playlist URL downloads multiple tracks; the direct path
	// must fetch exactly one, so reject sets and let the search pipeline handle it.
	return !strings.Contains(strings.ToLower(u.Path), "/sets/")
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

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefFloat(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefInt(p *int) int {
	if p == nil {
		return 0
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
		Duration:    derefFloat(track.DurationSeconds),
		ISRC:        derefStr(track.ISRC),
		Year:        derefInt(track.Year),
		TrackNumber: derefInt(track.TrackNumber),
		AlbumArtist: derefStr(track.AlbumArtist),
		Genre:       derefStr(track.Genre),
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
