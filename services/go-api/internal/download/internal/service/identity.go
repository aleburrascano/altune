package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
)

func (s *pipeline) resolveIdentity(ctx context.Context, ac *AcquisitionContext) {
	ResolveIdentity(ctx, s.recordings, ac)
}

func ResolveIdentity(ctx context.Context, recordings ports.RecordingResolver, ac *AcquisitionContext) {
	identity, err := recordings.Resolve(ctx, ac.Track.RecordingIdentity())
	if err != nil {
		slog.WarnContext(ctx, "acquisition.identity_resolve_failed",
			logging.KeyTrackID, ac.Track.ID, logging.KeyError, redact.LogError(err))
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
		s.cuts = CutsWithProvenMax(provenMaxBitErrorRate)
	}
}

func findingOf(verdict Verdict, basis string, score, bitErrorRate float64) Finding {
	if verdict.Kind == JudgedUnknown {
		return Finding{}
	}
	kind := verdictKindOf(verdict.Kind)
	finding := Finding{
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

func storedEvidenceOf(ctx context.Context, track *ports.TrackSnapshot) Evidence {
	if len(track.Evidence) == 0 {
		return Evidence{}
	}
	var stored Evidence
	if err := json.Unmarshal(track.Evidence, &stored); err != nil {
		slog.ErrorContext(ctx, "acquisition.stored_evidence_unreadable",
			logging.KeyTrackID, track.ID.String(), logging.KeyError, redact.LogError(err))
		return Evidence{}
	}
	return stored
}

func storedSourceOf(ctx context.Context, track *ports.TrackSnapshot) judgedSource {
	stored := storedEvidenceOf(ctx, track)
	kind := cmp.Or(stored.SourceKind, domain.FanUpload)
	if track.AudioSourceURL == nil || *track.AudioSourceURL == "" {
		kind = domain.UserUpload
	}
	return judgedSource{kind: kind, title: stored.SourceTitle}
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
