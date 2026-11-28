package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"log/slog"
)

type ReplaceService struct {
	*pipeline
}

func NewReplaceService(acquire *AcquireTrackAudioService) *ReplaceService {
	return &ReplaceService{pipeline: acquire.pipeline}
}

func (s *ReplaceService) ExecuteReplace(ctx context.Context, job ports.AcquisitionJob) error {
	ctx, cancel := s.acquireBudget(ctx, job)
	defer cancel()
	track, found, err := s.loadTrack(ctx, job.UserID, job.TrackID)
	if err != nil || !found {
		return err
	}
	_, err = s.replaceLoaded(ctx, job, track, ReplacementGate{}, WillRetry)
	return err
}

func (s *ReplaceService) ReplaceGated(ctx context.Context, job ports.AcquisitionJob, track *ports.TrackSnapshot, gate ReplacementGate) (ReplaceAttempt, error) {
	ctx, cancel := s.acquireBudget(ctx, job)
	defer cancel()
	ac, err := s.forUpgradeSweep().replaceLoaded(ctx, job, track, gate, neverRetry)
	return attemptOf(ac, err), err
}

func (s *ReplaceService) forUpgradeSweep() *ReplaceService {
	sweep := *s.pipeline
	sweep.events = events.NoopPublisher()
	sweep.rerecordings = ports.NoopRerecordingFinder()
	return &ReplaceService{pipeline: &sweep}
}

func attemptOf(ac *AcquisitionContext, err error) ReplaceAttempt {
	attempt := ReplaceAttempt{NoSource: err != nil && foundNoSource(err)}
	if ac != nil && ac.Selected != nil {
		attempt.SourceKey = sourceKey(ac.Selected.URL)
		attempt.Grade = ac.CollectEvidence().Grade
	}
	return attempt
}

func (s *ReplaceService) replaceLoaded(ctx context.Context, job ports.AcquisitionJob, track *ports.TrackSnapshot, gate ReplacementGate, retry retryPolicy) (*AcquisitionContext, error) {
	trackId := track.ID
	job.TrackID = trackId
	ac := s.startAcquisition(ctx, job, track)
	ac.Replace.Gate = gate
	ac.Replace.StoredKnownWrong = storedFileKnownWrong(track)
	defer s.observe(ac)
	defer CleanupTemp(ctx, s.workspace, ac)
	if err := configureReplaceExclusion(ctx, ac, track, trackId); err != nil {
		return ac, s.outcomes().settleReplace(ctx, err, retry, ac)
	}
	s.storedFiles().fingerprint(ctx, ac, track)
	s.loadPriorRejections(ctx, trackId, ac)
	if err := s.runAcquisition(ctx, ac); err != nil {
		return ac, s.outcomes().settleReplace(ctx, err, retry, ac)
	}
	s.supersededAudio().release(ctx, job.UserID, trackId, ac)
	return ac, nil
}

func storedFileKnownWrong(track *ports.TrackSnapshot) bool {
	catalog, stored := deref(track.CatalogDurationSeconds), deref(track.StoredDurationSeconds)
	if catalog > 0 && stored > 0 && !SameRecording(catalog, stored) {
		return true
	}
	var evidence Evidence
	return json.Unmarshal(track.Evidence, &evidence) == nil && evidence.Grade == domain.F
}

func configureReplaceExclusion(ctx context.Context, ac *AcquisitionContext, track *ports.TrackSnapshot, trackId ids.TrackId) error {
	ac.preserveCurrentAudio(track)
	if track.AudioSourceURL == nil {
		slog.InfoContext(ctx, "acquisition.replacing_unknown_source", logging.KeyTrackID, trackId.String())
	}
	if len(ac.Replace.ExcludeKeys) > 0 {
		slog.InfoContext(ctx, "acquisition.replacing_source",
			logging.KeyTrackID, trackId.String(), "excluded_keys", ac.Replace.ExcludeKeys)
	}
	return requireCurrentFileFact(track)
}

func requireCurrentFileFact(track *ports.TrackSnapshot) error {
	if sourceKey(deref(track.AudioSourceURL)) != "" || deref(track.StoredDurationSeconds) > 0 {
		return nil
	}
	return ErrReplaceFactMissing
}
