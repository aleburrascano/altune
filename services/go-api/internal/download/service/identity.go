package service

import (
	"altune/go-api/internal/download/internal/core/audioquality"
	"altune/go-api/internal/download/internal/core/audioverdict"
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service/replacement"
	"altune/go-api/internal/shared/redact"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
)

func (s *AcquireTrackAudioService) resolveIdentity(ctx context.Context, ac *AcquisitionContext) {
	ResolveIdentity(ctx, s.recordings, ac)
}

func ResolveIdentity(ctx context.Context, recordings ports.RecordingResolver, ac *AcquisitionContext) {
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
	return identity
}

var ErrInPlaceUnavailable = errors.New("the stored file cannot be checked in place: no stored-audio reader or identifier is wired")

const (
	basisAcoustID = "acoustid"
	basisPreview  = "preview"

	verdictPreview = "preview"
)

func WithProvenMaxBitErrorRate(provenMaxBitErrorRate float64) func(*AcquireTrackAudioService) {
	return func(s *AcquireTrackAudioService) {
		s.cuts = audioverdict.CutsWithProvenMax(provenMaxBitErrorRate)
	}
}

func (s *AcquireTrackAudioService) CheckInPlace(ctx context.Context, track *ports.TrackSnapshot) (replacement.Finding, error) {
	if track.AudioRef == nil {
		return replacement.Finding{}, ErrInPlaceUnavailable
	}
	finding, err := s.checkStoredFile(ctx, track)
	if errors.Is(err, ports.ErrNoStoredAudioReader) || errors.Is(err, ports.ErrNoAudioIdentifier) {
		return replacement.Finding{}, ErrInPlaceUnavailable
	}
	return finding, err
}

func (s *AcquireTrackAudioService) checkStoredFile(ctx context.Context, track *ports.TrackSnapshot) (replacement.Finding, error) {
	dir, err := s.workspace.Create("in-place-*")
	if err != nil {
		return replacement.Finding{}, fmt.Errorf("create in-place dir: %w", err)
	}
	defer removeScratch(ctx, s.workspace, dir)
	path, err := s.storedAudio.CopyStored(ctx, *track.AudioRef, dir)
	if err != nil {
		return replacement.Finding{}, fmt.Errorf("copy stored audio: %w", err)
	}
	ac := &AcquisitionContext{Track: buildTrackRef(track)}
	s.resolveIdentity(ctx, ac)
	return s.judgeInPlace(ctx, ac, track, path)
}

func (s *AcquireTrackAudioService) judgeInPlace(ctx context.Context, ac *AcquisitionContext, track *ports.TrackSnapshot, path string) (replacement.Finding, error) {
	input := judgeInput(ac, nil, deref(track.StoredDurationSeconds), storedSourceOf(ctx, track))
	score, err := s.fingerprintInto(ctx, ac, &input, path)
	if err != nil {
		return replacement.Finding{}, err
	}
	verdict := audioverdict.Judge(input, s.cuts)
	if verdict.Kind == audioverdict.Proven {
		return findingOf(verdict, basisAcoustID, score, 0), nil
	}
	preview := s.previewInPlace(ctx, track, path)
	if preview == nil {
		return findingOf(verdict, basisAcoustID, score, 0), nil
	}
	input.Audio.Preview = preview
	return findingOf(audioverdict.Judge(input, s.cuts), basisPreview, score, preview.BitErrorRate), nil
}

func (s *AcquireTrackAudioService) fingerprintInto(ctx context.Context, ac *AcquisitionContext, input *audioverdict.Input, path string) (float64, error) {
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
	input.Audio.RecordingFacts = versionFactsOf(ctx, newRecordingFactsCache(s.now), s.recordingLookup, *input)
	return topScore(match.Results), nil
}

func findingOf(verdict audioverdict.Verdict, basis string, score, bitErrorRate float64) replacement.Finding {
	if verdict.Kind == audioverdict.Unknown {
		return replacement.Finding{}
	}
	kind := verdictKindOf(verdict.Kind)
	finding := replacement.Finding{
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

func storedSourceOf(ctx context.Context, track *ports.TrackSnapshot) judgedSource {
	var stored Evidence
	if len(track.Evidence) > 0 {
		if err := json.Unmarshal(track.Evidence, &stored); err != nil {
			slog.ErrorContext(ctx, "acquisition.stored_evidence_unreadable",
				"track_id", track.ID.String(), "error", redact.LogError(err))
			stored = Evidence{}
		}
	}
	kind := cmp.Or(stored.SourceKind, audioquality.FanUpload)
	if track.AudioSourceURL == nil || *track.AudioSourceURL == "" {
		kind = audioquality.UserUpload
	}
	return judgedSource{kind: kind, title: stored.SourceTitle}
}

func (s *AcquireTrackAudioService) previewInPlace(ctx context.Context, track *ports.TrackSnapshot, path string) *audioverdict.Preview {
	target := previewTargetOf(track)
	if target == (ports.PreviewTarget{}) {
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
	return &audioverdict.Preview{BitErrorRate: comparison.BitErrorRate, OverlapSeconds: comparison.OverlapSeconds}
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
