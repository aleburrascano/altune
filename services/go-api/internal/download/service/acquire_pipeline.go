package service

import (
	"altune/go-api/internal/download/internal/core/audioverdict"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/random"
	"altune/go-api/internal/shared/redact"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type pipeline struct {
	ledger          ports.LibraryTracks
	sources         *SourceRegistry
	audioStore      ports.AudioWriter
	audioProber     ports.AudioProber
	audioTagger     ports.AudioTagger
	identifier      ports.AudioIdentifier
	recordings      ports.RecordingResolver
	rerecordings    ports.RerecordingFinder
	events          events.Publisher
	orphans         ports.OrphanedAudioRecorder
	storeKeyPrefix  string
	rejections      ports.RejectionStore
	downloadLimiter *ConcurrencyLimit
	recordingLookup ports.RecordingLookup
	verifySkips     ports.VerifySkipRecorder
	confidenceFloor float64
	workspace       ports.ScratchFolder
	storedAudio     ports.StoredAudioReader
	contextObserver func(*AcquisitionContext)
	sleep           clock.Sleeper
	now             clock.Func
	unit            random.Unit
	previews        ports.PreviewMatcher

	cuts            audioverdict.Cuts
	acquireTimeout  time.Duration
	supersededGrace time.Duration
}

func (s *pipeline) outcomes() outcomeReporter {
	return outcomeReporter{ledger: s.ledger, events: s.events}
}

func (s *pipeline) supersededAudio() supersededAudio {
	return supersededAudio{ledger: s.ledger, orphans: s.orphans, grace: s.supersededGrace, now: s.now}
}

func (s *pipeline) storedFiles() storedFileFingerprinter {
	return storedFileFingerprinter{workspace: s.workspace, reader: s.storedAudio, identifier: s.identifier}
}

func (s *pipeline) observe(ac *AcquisitionContext) {
	s.contextObserver(ac)
}

func (s *pipeline) acquireBudget(ctx context.Context, job ports.AcquisitionJob) (context.Context, context.CancelFunc) {
	ctx = logging.ContextWithSubjectUser(ctx, job.UserID.String())
	return context.WithTimeoutCause(ctx, s.acquireTimeout, errAcquireBudgetSpent)
}

func (s *pipeline) loadTrack(ctx context.Context, userId ids.UserId, trackId ids.TrackId) (*ports.TrackSnapshot, bool, error) {
	track, err := s.ledger.Get(ctx, trackId, userId)
	if errors.Is(err, ports.ErrTrackNotFound) {
		slog.WarnContext(ctx, "acquire_track_not_found", logging.KeyTrackID, trackId.String())
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get track: %w", err)
	}
	return track, true, nil
}

func (s *pipeline) startAcquisition(ctx context.Context, job ports.AcquisitionJob, track *ports.TrackSnapshot) *AcquisitionContext {
	job.Progress().Meta(track.Title, track.Artist, track.Album)

	slog.InfoContext(ctx, "track_acquisition_started",
		logging.KeyTrackID, job.TrackID.String(),
		"has_isrc", track.ISRC != nil,
	)
	s.events.Publish(ctx, job.UserID, events.TrackAcquisitionStarted{
		TrackID: job.TrackID.String(),
		Title:   track.Title,
		Artist:  track.Artist,
	})
	return &AcquisitionContext{
		Job:     job,
		Track:   buildTrackRef(track),
		Replace: ReplaceState{ExcludeKeys: mergeSourceKeys(track.RejectedSourceKeys)},
	}
}

func (s *pipeline) runAcquisition(ctx context.Context, ac *AcquisitionContext) error {
	defer s.recordRejections(ctx, ac)
	defer reportDecision(ac)
	identityCtx, identityDone := startStage(ctx, ac, s.now, "identity")
	s.resolveIdentity(identityCtx, ac)
	identityDone(nil)
	steps := s.buildSteps(ac.Job.UserID, ac.Job.TrackID)
	failure := RunPipeline(ctx, steps, ac)
	if failure != nil {
		failure = s.takeRerecording(ctx, steps, ac, failure)
	}
	if failure != nil {
		return failure
	}

	ac.Job.Progress().Fetched(ac.SourceFormat)
	ac.Job.Progress().Provenance(string(ac.Provenance()))
	s.outcomes().completed(ctx, ac.Job.UserID, ac.Job.TrackID, ac.AudioRef)
	return nil
}

func (s *pipeline) takeRerecording(ctx context.Context, steps Pipeline, ac *AcquisitionContext, originalErr error) error {
	originalMBID := cmp.Or(ac.Identity.MBID, ac.Track.CatalogMBID)
	if !s.mayTakeRerecording(ctx, ac, originalErr) || originalMBID == "" {
		return originalErr
	}
	label, found := s.findRerecording(ctx, ac, originalMBID)
	if !found {
		return originalErr
	}
	err := s.runRerecordingPass(ctx, steps, ac, label)
	if err == nil {
		logRerecordingTier(ctx, ac, originalMBID, label.MBID, "taken", nil)
		return nil
	}
	logRerecordingTier(ctx, ac, originalMBID, label.MBID, "pass_failed", err)
	if ctx.Err() != nil {
		return err
	}
	return originalErr
}

func (s *pipeline) runRerecordingPass(ctx context.Context, steps Pipeline, ac *AcquisitionContext, label ports.TrackLabel) error {
	pass := ac.Snapshot()
	pass.retargetTo(label)
	s.resolveIdentity(ctx, &pass)
	if err := RunPipeline(ctx, steps, &pass); err != nil {
		return err
	}
	s.recordRejections(ctx, ac)
	pass.Rejections = pass.Rejections[len(ac.Rejections):]
	*ac = pass
	return nil
}

func (s *pipeline) mayTakeRerecording(ctx context.Context, ac *AcquisitionContext, err error) bool {
	return noSourceHasIt(err) && ctx.Err() == nil && ac.Replace.allowsRerecording()
}

func (s *pipeline) findRerecording(ctx context.Context, ac *AcquisitionContext, originalMBID string) (ports.TrackLabel, bool) {
	label, found, err := s.rerecordings.FindRerecording(ctx, ports.RerecordingQuery{OriginalMBID: originalMBID})
	switch {
	case err != nil:
		logRerecordingTier(ctx, ac, originalMBID, "", "finder_failed", err)
	case !found:
		logRerecordingTier(ctx, ac, originalMBID, "", "none", nil)
	case label.MBID == ac.Track.CatalogMBID:
		logRerecordingTier(ctx, ac, originalMBID, label.MBID, "already_the_rerecording", nil)
	default:
		return label, true
	}
	return ports.TrackLabel{}, false
}

func logRerecordingTier(ctx context.Context, ac *AcquisitionContext, originalMBID, rerecordingMBID, outcome string, err error) {
	attrs := []any{logging.KeyTrackID, ac.Track.ID, "original_mbid", originalMBID, "rerecording_mbid", rerecordingMBID, "outcome", outcome}
	if err != nil {
		slog.WarnContext(ctx, "acquisition.rerecording_tier", append(attrs, logging.KeyError, redact.LogError(err))...)
		return
	}
	slog.InfoContext(ctx, "acquisition.rerecording_tier", attrs...)
}

func reportDecision(ac *AcquisitionContext) {
	var key string
	if ac.Selected != nil {
		key = sourceKey(ac.Selected.URL)
	}
	ac.Job.Progress().Decided(ac.Confidence, key, ac.ExpectedLength(), ac.StoredLength().Seconds())
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
		ISRC:        deref(track.ISRC),
		Year:        deref(track.Year),
		TrackNumber: deref(track.TrackNumber),
		AlbumArtist: deref(track.AlbumArtist),
		Genre:       deref(track.Genre),

		CatalogDuration: deref(track.CatalogDurationSeconds),
		CatalogExplicit: track.CatalogExplicit,
		CatalogMBID:     deref(track.CatalogMBID),
		CatalogSources:  track.CatalogSources,

		AudioSourceURL: deref(track.AudioSourceURL),
	}
}

func mergeSourceKeys(existing []string, add ...string) []string {
	merged := make([]string, 0, len(existing)+len(add))
	seen := make(map[string]bool, len(existing)+len(add))
	for _, key := range append(append([]string{}, existing...), add...) {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, key)
	}
	return merged
}
