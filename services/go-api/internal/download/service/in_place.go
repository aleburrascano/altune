package service

import (
	"altune/go-api/internal/download/internal/core/audioverdict"
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service/replacement"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

type InPlaceChecker struct {
	workspace       ports.ScratchFolder
	storedAudio     ports.StoredAudioReader
	recordings      ports.RecordingResolver
	identifier      ports.AudioIdentifier
	previews        ports.PreviewMatcher
	recordingLookup ports.RecordingLookup
	now             clock.Func
	cuts            audioverdict.Cuts
}

func NewInPlaceChecker(acquire *AcquireTrackAudioService) *InPlaceChecker {
	p := acquire.pipeline
	return &InPlaceChecker{
		workspace:       p.workspace,
		storedAudio:     p.storedAudio,
		recordings:      p.recordings,
		identifier:      p.identifier,
		previews:        p.previews,
		recordingLookup: p.recordingLookup,
		now:             p.now,
		cuts:            p.cuts,
	}
}

func (c *InPlaceChecker) CheckInPlace(ctx context.Context, track *ports.TrackSnapshot) (replacement.Finding, error) {
	if track.AudioRef == nil {
		return replacement.Finding{}, ErrInPlaceUnavailable
	}
	finding, err := c.checkStoredFile(ctx, track)
	if errors.Is(err, ports.ErrNoStoredAudioReader) || errors.Is(err, ports.ErrNoAudioIdentifier) {
		return replacement.Finding{}, ErrInPlaceUnavailable
	}
	return finding, err
}

func (c *InPlaceChecker) checkStoredFile(ctx context.Context, track *ports.TrackSnapshot) (replacement.Finding, error) {
	dir, err := c.workspace.Create("in-place-*")
	if err != nil {
		return replacement.Finding{}, fmt.Errorf("create in-place dir: %w", err)
	}
	defer removeScratch(ctx, c.workspace, dir)
	path, err := c.storedAudio.CopyStored(ctx, *track.AudioRef, dir)
	if err != nil {
		return replacement.Finding{}, fmt.Errorf("copy stored audio: %w", err)
	}
	ac := &AcquisitionContext{Track: buildTrackRef(track)}
	ResolveIdentity(ctx, c.recordings, ac)
	return c.judgeInPlace(ctx, ac, track, path)
}

func (c *InPlaceChecker) judgeInPlace(ctx context.Context, ac *AcquisitionContext, track *ports.TrackSnapshot, path string) (replacement.Finding, error) {
	input := judgeInput(ac, nil, deref(track.StoredDurationSeconds), storedSourceOf(ctx, track))
	score, err := c.fingerprintInto(ctx, ac, &input, path)
	if err != nil {
		return replacement.Finding{}, err
	}
	verdict := audioverdict.Judge(input, c.cuts)
	if verdict.Kind == audioverdict.Proven {
		return findingOf(verdict, basisAcoustID, score, 0), nil
	}
	preview := c.previewInPlace(ctx, track, path)
	if preview == nil {
		return findingOf(verdict, basisAcoustID, score, 0), nil
	}
	input.Audio.Preview = preview
	return findingOf(audioverdict.Judge(input, c.cuts), basisPreview, score, preview.BitErrorRate), nil
}

func (c *InPlaceChecker) fingerprintInto(ctx context.Context, ac *AcquisitionContext, input *audioverdict.Input, path string) (float64, error) {
	if ac.Identity.MBID == "" {
		return 0, nil
	}
	match, err := c.identifier.Identify(ctx, path, 0)
	if errors.Is(err, acqdomain.ErrEmptyFingerprint) {
		slog.WarnContext(ctx, "acquisition.in_place_fingerprint_empty",
			logging.KeyTrackID, ac.Track.ID, logging.KeyError, redact.LogError(err))
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("fingerprint the stored file: %w", err)
	}
	input.Audio.AcoustID = match.Results
	input.Audio.RecordingFacts = versionFactsOf(ctx, newRecordingFactsCache(c.now), c.recordingLookup, *input)
	return topScore(match.Results), nil
}

func (c *InPlaceChecker) previewInPlace(ctx context.Context, track *ports.TrackSnapshot, path string) *audioverdict.Preview {
	target := previewTargetOf(track)
	if target == (ports.PreviewTarget{}) {
		return nil
	}
	comparison, err := c.previews.MatchesPreview(ctx, path, target)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.in_place_preview_failed",
			logging.KeyTrackID, track.ID.String(), logging.KeyError, redact.LogError(err))
		return nil
	}
	if comparison.Unavailable != "" {
		return nil
	}
	return &audioverdict.Preview{BitErrorRate: comparison.BitErrorRate, OverlapSeconds: comparison.OverlapSeconds}
}
