package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/grade"
	"altune/go-api/internal/acquisition/service/upgrade"
	"altune/go-api/internal/shared/redact"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
)

func (s *AcquireTrackAudioService) resolveIdentity(ctx context.Context, ac *AcquisitionContext) {
	ResolveIdentity(ctx, s.recordings, s.identifier, ac)
}

func ResolveIdentity(ctx context.Context, recordings ports.RecordingResolver, _ ports.AudioIdentifier, ac *AcquisitionContext) {
	identity, err := recordings.Resolve(ctx, ports.RecordingQuery{
		Title:    ac.Track.Title,
		Artist:   ac.Track.Artist,
		Album:    ac.Track.Album,
		ISRC:     ac.Track.ISRC,
		Duration: ac.Track.CatalogDuration,
		MBID:     ac.Track.CatalogMBID,
	})
	if err != nil {
		slog.WarnContext(ctx, "acquisition.identity_resolve_failed",
			"track_id", ac.Track.ID, "error", redact.LogError(err))
	}
	identity = withCatalogPin(identity, ac.Track.CatalogMBID)
	if identity.IsZero() {
		return
	}
	ac.Identity = identity
}

func withCatalogPin(identity ports.RecordingIdentity, pin string) ports.RecordingIdentity {
	if pin == "" || identity.MBID != "" {
		return identity
	}
	identity.MBID = pin
	identity.MBIDs = []string{pin}
	return identity
}

var ErrInPlaceUnavailable = errors.New("the stored file cannot be checked in place: no stored-audio reader or identifier is wired")

const (
	basisAcoustID = "acoustid"
	basisPreview  = "preview"

	verdictPreview = "preview"
)

func WithPreviewMatcher(m ports.PreviewMatcher, officialMaxBitErrorRate float64) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) {
		s.previews = m
		s.previewOfficialMaxBER = officialMaxBitErrorRate
	}
}

func (s *AcquireTrackAudioService) CheckInPlace(ctx context.Context, track *ports.TrackSnapshot) (upgrade.Finding, error) {
	if s.storedAudio == nil || s.identifier == nil || track.AudioRef == nil {
		return upgrade.Finding{}, ErrInPlaceUnavailable
	}
	ac := &AcquisitionContext{Track: buildTrackRef(track)}
	s.resolveIdentity(ctx, ac)
	dir, err := s.workspace.Create("in-place-*")
	if err != nil {
		return upgrade.Finding{}, fmt.Errorf("create in-place dir: %w", err)
	}
	defer func() { _ = s.workspace.Remove(dir) }()
	path, err := s.storedAudio.CopyStored(ctx, *track.AudioRef, dir)
	if err != nil {
		return upgrade.Finding{}, fmt.Errorf("copy stored audio: %w", err)
	}
	return s.judgeInPlace(ctx, ac, track, path)
}

func (s *AcquireTrackAudioService) judgeInPlace(ctx context.Context, ac *AcquisitionContext, track *ports.TrackSnapshot, path string) (upgrade.Finding, error) {
	finding, err := s.fingerprintInPlace(ctx, ac, path, deref(track.StoredDurationSeconds))
	if err != nil || isProven(finding) {
		return finding, err
	}
	if preview, matched := s.previewInPlace(ctx, ac, track, path); matched {
		return preview, nil
	}
	if namesAnotherBoundRecording(ac) {
		return finding, nil
	}
	return upgrade.Finding{}, nil
}

func isProven(finding upgrade.Finding) bool {
	return finding.Fingerprint == grade.Matches || finding.Fingerprint == grade.Approximates
}

func namesAnotherBoundRecording(ac *AcquisitionContext) bool {
	reference := ac.Identity.MBIDs
	if len(reference) == 0 {
		reference = []string{ac.Identity.MBID}
	}
	return slices.ContainsFunc(ac.Verdict.Surviving, func(recording ports.LinkedRecording) bool {
		return recording.MBID != "" && !slices.Contains(reference, recording.MBID)
	})
}

func (s *AcquireTrackAudioService) fingerprintInPlace(ctx context.Context, ac *AcquisitionContext, path string, storedSeconds float64) (upgrade.Finding, error) {
	if ac.Identity.MBID == "" {
		return upgrade.Finding{}, nil
	}
	match, err := s.identifier.Identify(ctx, path, 0)
	if err != nil {
		return upgrade.Finding{}, fmt.Errorf("fingerprint the stored file: %w", err)
	}
	classifier := NewDownloadStep(nil, WithRecordingLookup(s.recordingLookup))
	ac.Verdict = classifier.classifyMatch(ctx, ac, match, storedSeconds)
	return upgrade.Finding{
		Verdict:     string(ac.Verdict.Kind),
		Fingerprint: ac.gradedFingerprint(),
		Basis:       basisAcoustID,
		Score:       ac.Verdict.Score,
	}, nil
}

func (s *AcquireTrackAudioService) previewInPlace(ctx context.Context, ac *AcquisitionContext, track *ports.TrackSnapshot, path string) (upgrade.Finding, bool) {
	target := previewTargetOf(track)
	if s.previews == nil || (target == ports.PreviewTarget{}) {
		return upgrade.Finding{}, false
	}
	comparison, err := s.previews.MatchesPreview(ctx, path, target)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.in_place_preview_failed",
			"track_id", track.ID.String(), "error", redact.LogError(err))
		return upgrade.Finding{}, false
	}
	if !comparison.Matched {
		return upgrade.Finding{}, false
	}
	return upgrade.Finding{
		Verdict:      verdictPreview,
		Fingerprint:  s.previewFingerprint(ac, comparison.BitErrorRate),
		Basis:        basisPreview,
		BitErrorRate: comparison.BitErrorRate,
	}, true
}

func (s *AcquireTrackAudioService) previewFingerprint(ac *AcquisitionContext, bitErrorRate float64) grade.Fingerprint {
	if ac.Identity.ReferenceDoubted || bitErrorRate > s.previewOfficialMaxBER {
		return grade.Approximates
	}
	return grade.Matches
}

func previewTargetOf(track *ports.TrackSnapshot) ports.PreviewTarget {
	target := ports.PreviewTarget{ISRC: deref(track.ISRC)}
	for _, source := range track.CatalogSources {
		if source.Provider == string(ports.ProviderDeezer) {
			target.DeezerTrackID = source.ExternalID
		}
	}
	return target
}
