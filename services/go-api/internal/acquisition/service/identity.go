package service

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/audiojudge"
	"altune/go-api/internal/acquisition/service/grade"
	"altune/go-api/internal/acquisition/service/upgrade"
	"altune/go-api/internal/shared/redact"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

func WithPreviewMatcher(m ports.PreviewMatcher, provenMaxBitErrorRate float64) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) {
		s.previews = m
		s.cuts = audiojudge.CutsWithProvenMax(provenMaxBitErrorRate)
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
	input := judgeInput(ac, nil, deref(track.StoredDurationSeconds), storedSourceOf(track))
	score, err := s.fingerprintInto(ctx, ac, &input, path)
	if err != nil {
		return upgrade.Finding{}, err
	}
	verdict := audiojudge.Judge(input, s.cuts)
	if verdict.Kind == audiojudge.Proven {
		return findingOf(verdict, basisAcoustID, score, 0), nil
	}
	preview := s.previewInPlace(ctx, track, path)
	if preview == nil {
		return findingOf(verdict, basisAcoustID, score, 0), nil
	}
	input.Audio.Preview = preview
	return findingOf(audiojudge.Judge(input, s.cuts), basisPreview, score, preview.BitErrorRate), nil
}

func (s *AcquireTrackAudioService) fingerprintInto(ctx context.Context, ac *AcquisitionContext, input *audiojudge.Input, path string) (float64, error) {
	if ac.Identity.MBID == "" {
		return 0, nil
	}
	match, err := s.identifier.Identify(ctx, path, 0)
	if errors.Is(err, acqdomain.ErrEmptyFingerprint) {
		slog.WarnContext(ctx, "acquisition.in_place_fingerprint_empty",
			"track_id", ac.Track.ID, "error", redact.LogError(err))
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("fingerprint the stored file: %w", err)
	}
	input.Audio.AcoustID = match.Results
	input.Audio.RecordingFacts = NewDownloadStep(nil, WithRecordingLookup(s.recordingLookup)).versionFacts(ctx, *input)
	return topScore(match.Results), nil
}

func findingOf(verdict audiojudge.Verdict, basis string, score, bitErrorRate float64) upgrade.Finding {
	if verdict.Kind == audiojudge.Unknown {
		return upgrade.Finding{}
	}
	kind := verdictKindOf(verdict.Kind)
	finding := upgrade.Finding{
		Verdict:      string(kind),
		Fingerprint:  FingerprintOf(kind),
		Basis:        basis,
		Score:        score,
		Lowers:       verdict.LowersStoredGrade,
		BitErrorRate: bitErrorRate,
	}
	if basis == basisPreview {
		finding.Verdict = verdictPreview
	}
	return finding
}

func storedSourceOf(track *ports.TrackSnapshot) judgedSource {
	var stored Evidence
	_ = json.Unmarshal(track.Evidence, &stored)
	kind := cmp.Or(stored.SourceKind, grade.FanUpload)
	if track.AudioSourceURL == nil || *track.AudioSourceURL == "" {
		kind = grade.UserUpload
	}
	return judgedSource{kind: kind, title: stored.SourceTitle}
}

func (s *AcquireTrackAudioService) previewInPlace(ctx context.Context, track *ports.TrackSnapshot, path string) *audiojudge.Preview {
	target := previewTargetOf(track)
	if s.previews == nil || (target == ports.PreviewTarget{}) {
		return nil
	}
	comparison, err := s.previews.MatchesPreview(ctx, path, target)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.in_place_preview_failed",
			"track_id", track.ID.String(), "error", redact.LogError(err))
		return nil
	}
	if comparison.Unavailable != "" {
		return nil
	}
	return &audiojudge.Preview{BitErrorRate: comparison.BitErrorRate, OverlapSeconds: comparison.OverlapSeconds}
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
