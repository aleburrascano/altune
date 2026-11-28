package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"math"
)

const (
	defaultConfidenceFloor = 0.5
	confidenceScale        = 100

	hardConfidence      = 0.95
	softConfidence      = 0.85
	lowScoreCutoff      = 0.92
	lowScorePenalty     = 0.10
	unknownBase         = 0.2
	unknownCap          = 0.8
	trustedChannelBonus = 0.3
	tightDurationBonus  = 0.2
	looseDurationBonus  = 0.1
	resolvedBonus       = 0.1
	qualifierPenalty    = 0.2
)

func ScoreConfidence(e Evidence, trackSeconds float64) float64 {
	switch VerdictKind(e.Verdict) {
	case VerdictHard:
		return fingerprintConfidence(hardConfidence, e.AcoustIDScore)
	case VerdictSoft:
		return fingerprintConfidence(softConfidence, e.AcoustIDScore)
	case VerdictOtherVersion, VerdictDifferentSong, VerdictUnknown, VerdictContradicted:
		return unknownConfidence(e, trackSeconds)
	}
	return unknownConfidence(e, trackSeconds)
}

func fingerprintConfidence(base, score float64) float64 {
	if score > 0 && score < lowScoreCutoff {
		return math.Max(base-lowScorePenalty, unknownCap)
	}
	return base
}

func unknownConfidence(e Evidence, trackSeconds float64) float64 {
	total := unknownBase + channelBonus(e.Channel) + durationBonus(e.DurationDeltaSeconds, trackSeconds)
	if e.Resolved {
		total += resolvedBonus
	}
	if len(e.Qualifiers) > 0 {
		total -= qualifierPenalty
	}
	return math.Round(total*confidenceScale) / confidenceScale
}

func channelBonus(class string) float64 {
	if class == ChannelTopic || class == ChannelArtist {
		return trustedChannelBonus
	}
	return 0
}

func durationBonus(delta *float64, trackSeconds float64) float64 {
	if delta == nil {
		return 0
	}
	off := math.Abs(*delta)
	switch {
	case off <= domain.LengthTight.Allowance(trackSeconds):
		return tightDurationBonus
	case off <= domain.LengthLoose.Allowance(trackSeconds):
		return looseDurationBonus
	default:
		return 0
	}
}

func (ac *AcquisitionContext) buildEvidence(candidate ports.AudioCandidate, verified verificationResult) Evidence {
	_, qualifiers := UnrequestedQualifiers(ac.Track.Title, ac.Track.Artist, candidate.Title)
	var delta *float64
	if d, known := ac.durationDelta(candidate, verified.probed.Seconds()); known {
		delta = &d
	}
	return Evidence{
		Verdict:              string(verified.verdict.Kind),
		AcoustIDScore:        verified.verdict.Score,
		DurationDeltaSeconds: delta,
		Channel:              UploaderOf(ac.Track.Artist, candidate),
		Qualifiers:           qualifiers,
		Resolved:             candidate.IsResolved(),
		SourceTitle:          candidate.Title,
	}
}

func (ac *AcquisitionContext) durationDelta(candidate ports.AudioCandidate, probed float64) (float64, bool) {
	expected := ac.ExpectedLength()
	if probed <= 0 || expected <= 0 {
		return 0, false
	}
	return probed - expected, true
}

func (ac *AcquisitionContext) adopt(ctx context.Context, evidence Evidence, confidence float64) {
	ac.Evidence = evidence
	ac.Confidence = confidence
	slog.InfoContext(ctx, "acquisition.confidence",
		logging.KeyTrackID, ac.Track.ID,
		"confidence", ac.Confidence,
		"evidence", evidence,
	)
}

type Evidence struct {
	Verdict              string            `json:"verdict"`
	AcoustIDScore        float64           `json:"acoustid_score"`
	DurationDeltaSeconds *float64          `json:"duration_delta_seconds"`
	Channel              string            `json:"channel"`
	Qualifiers           []string          `json:"qualifiers"`
	Resolved             bool              `json:"resolved"`
	SourceTitle          string            `json:"source_title"`
	ISRCSource           ProofSource       `json:"isrc_source"`
	SourceKind           domain.SourceKind `json:"source_kind"`
	PipelineGeneration   int               `json:"pipeline_generation"`
	Grade                domain.Grade      `json:"grade"`
	SourceFormatID       string            `json:"source_format_id,omitempty"`
	SourceKbps           int               `json:"source_kbps,omitempty"`
}

func SourceKindOf(c ports.AudioCandidate, channelClass string) domain.SourceKind {
	switch c.Kind {
	case ports.KindArtTrack:
		return domain.ArtTrack
	case ports.KindPinnedVideo:
		return officialVideoKind(c)
	case ports.KindUpload:
		return uploadSourceKind(channelClass)
	case ports.KindCatalogPin:
		return domain.FanUpload
	}
	return domain.FanUpload
}

func officialVideoKind(c ports.AudioCandidate) domain.SourceKind {
	if c.ObservedOMV {
		return domain.OfficialVideo
	}
	return domain.FanUpload
}

func UploaderSourceKind(trackArtist, channel string) domain.SourceKind {
	return uploadSourceKind(UploaderOf(trackArtist, ports.AudioCandidate{Channel: channel, Kind: ports.KindUpload}))
}

func uploadSourceKind(channelClass string) domain.SourceKind {
	if channelClass == ChannelTopic || channelClass == ChannelArtist {
		return domain.LabelUpload
	}
	return domain.FanUpload
}

func FingerprintOf(verdict VerdictKind) domain.FingerprintResult {
	switch verdict {
	case VerdictHard:
		return domain.Matches
	case VerdictSoft:
		return domain.Approximates
	case VerdictOtherVersion, VerdictContradicted:
		return domain.Contradicts
	case VerdictDifferentSong:
		return domain.Unsupported
	case VerdictUnknown:
		return domain.Silent
	}
	return domain.Silent
}

func (ac *AcquisitionContext) CollectEvidence() Evidence {
	evidence := ac.Evidence
	_, evidence.ISRCSource = ac.provenISRC()
	if ac.Selected != nil {
		evidence.SourceKind = SourceKindOf(*ac.Selected, evidence.Channel)
	}
	evidence.PipelineGeneration = domain.PipelineGeneration
	evidence.SourceFormatID = ac.SourceFormat.ID
	evidence.SourceKbps = ac.SourceFormat.Kbps
	evidence.Grade = ac.gradeFor(evidence.SourceKind)
	return evidence
}

func (ac *AcquisitionContext) gradeFor(kind domain.SourceKind) domain.Grade {
	if ac.Verdict.Judged.Grade != "" {
		return ac.Verdict.Judged.Grade
	}
	return domain.Of(FingerprintOf(ac.Verdict.Kind), ac.Provenance(), kind)
}

func (ac *AcquisitionContext) provenISRC() (string, ProofSource) {
	return ProveISRC(ProofFacts{
		CatalogISRC: ac.Track.ISRC,
		CatalogMBID: ac.Track.CatalogMBID,
		Identity:    ac.Identity,
		HardVerdict: ac.Verdict.Kind == VerdictHard,
		Surviving:   ac.Verdict.Surviving,
	})
}
