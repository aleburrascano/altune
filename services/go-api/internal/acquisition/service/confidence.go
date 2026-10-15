package service

import (
	"altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/grade"
	"context"
	"log/slog"
	"math"
	"slices"
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

	channelTopic  = "topic"
	channelArtist = "artist"
	channelOther  = "other"
)

func ScoreConfidence(e Evidence, trackSeconds float64) float64 {
	switch VerdictKind(e.Verdict) {
	case VerdictHard:
		return fingerprintConfidence(hardConfidence, e.AcoustIDScore)
	case VerdictSoft:
		return fingerprintConfidence(softConfidence, e.AcoustIDScore)
	default:
		return unknownConfidence(e, trackSeconds)
	}
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
	return math.Round(math.Min(math.Max(total, 0), unknownCap)*confidenceScale) / confidenceScale
}

func channelBonus(class string) float64 {
	if class == channelTopic || class == channelArtist {
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
	_, fallback := UnrequestedQualifiers(ac.Track.Title, ac.Track.Artist, candidate.Title)
	var delta *float64
	if d, known := ac.durationDelta(candidate, verified.probed); known {
		delta = &d
	}
	return Evidence{
		Verdict:              string(verified.verdict.Kind),
		AcoustIDScore:        verified.verdict.Score,
		DurationDeltaSeconds: delta,
		Channel:              uploaderClass(ac.Track.Artist, candidate),
		Qualifiers:           fallback,
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

func fallbackLengthMatches(delta *float64, trackSeconds float64) bool {
	return delta != nil && math.Abs(*delta) <= domain.LengthAuthoritative.Allowance(trackSeconds)
}

func (ac *AcquisitionContext) adopt(ctx context.Context, evidence Evidence) {
	ac.Evidence = evidence
	ac.Confidence = ScoreConfidence(evidence, ac.ExpectedLength())
	slog.InfoContext(ctx, "acquisition.confidence",
		"track_id", ac.Track.ID,
		"confidence", ac.Confidence,
		"evidence", evidence,
	)
}

type Evidence struct {
	Verdict              string           `json:"verdict"`
	AcoustIDScore        float64          `json:"acoustid_score"`
	ISRCSetHit           bool             `json:"isrc_set_hit"`
	DurationDeltaSeconds *float64         `json:"duration_delta_seconds"`
	Channel              string           `json:"channel"`
	Qualifiers           []string         `json:"qualifiers"`
	Resolved             bool             `json:"resolved"`
	SourceTitle          string           `json:"source_title"`
	ISRCSource           ISRCSource       `json:"isrc_source"`
	SourceKind           grade.SourceKind `json:"source_kind"`
	PipelineGeneration   int              `json:"pipeline_generation"`
	Grade                grade.Grade      `json:"grade"`
}

func SourceKindOf(c ports.AudioCandidate, channelClass string) grade.SourceKind {
	switch c.Kind {
	case ports.KindArtTrack:
		return grade.ArtTrack
	case ports.KindPinnedVideo:
		return officialVideoKind(c)
	case ports.KindUpload:
		return uploadSourceKind(channelClass)
	default:
		return grade.FanUpload
	}
}

func officialVideoKind(c ports.AudioCandidate) grade.SourceKind {
	if c.ObservedOMV {
		return grade.OfficialVideo
	}
	return grade.FanUpload
}

func UploaderSourceKind(trackArtist, channel string) grade.SourceKind {
	return uploadSourceKind(uploaderClass(trackArtist, ports.AudioCandidate{Channel: channel, Kind: ports.KindUpload}))
}

func uploadSourceKind(channelClass string) grade.SourceKind {
	if channelClass == channelTopic || channelClass == channelArtist {
		return grade.LabelUpload
	}
	return grade.FanUpload
}

func (ac *AcquisitionContext) gradedFingerprint() grade.Fingerprint {
	fingerprint := FingerprintOf(ac.Verdict.Kind)
	if fingerprint == grade.Matches && ac.Identity.ReferenceDoubted {
		return grade.Approximates
	}
	return fingerprint
}

func FingerprintOf(verdict VerdictKind) grade.Fingerprint {
	switch verdict {
	case VerdictHard:
		return grade.Matches
	case VerdictSoft:
		return grade.Approximates
	case VerdictOtherVersion:
		return grade.Contradicts
	case VerdictDifferentSong:
		return grade.Unsupported
	default:
		return grade.Silent
	}
}

func (ac *AcquisitionContext) collectEvidence() Evidence {
	evidence := ac.Evidence
	evidence.Verdict = string(ac.Verdict.Kind)
	evidence.AcoustIDScore = ac.Verdict.Score
	_, evidence.ISRCSource = ac.provenISRC()
	if ac.Selected != nil {
		evidence.SourceTitle = ac.Selected.Title
		evidence.SourceKind = SourceKindOf(*ac.Selected, evidence.Channel)
	}
	evidence.PipelineGeneration = grade.PipelineGeneration
	evidence.Grade = grade.Of(ac.gradedFingerprint(), ac.Provenance(), evidence.SourceKind)
	return evidence
}

type ISRCSource string

const (
	ISRCFromCatalog         ISRCSource = "catalog"
	ISRCFromPinnedRecording ISRCSource = "pinned_recording"
	ISRCFromFingerprint     ISRCSource = "fingerprint"
	ISRCUnproven            ISRCSource = "unproven"
)

func (ac *AcquisitionContext) provenISRC() (string, ISRCSource) {
	switch {
	case ac.Track.ISRC != "":
		return ac.Track.ISRC, ISRCFromCatalog
	case ac.Identity.ISRC == "":
		return "", ISRCUnproven
	case ac.identityIsCatalogPin():
		return ac.Identity.ISRC, ISRCFromPinnedRecording
	case ac.fingerprintProvesReference():
		return ac.Identity.ISRC, ISRCFromFingerprint
	}
	return "", ISRCUnproven
}

func (ac *AcquisitionContext) identityIsCatalogPin() bool {
	return ac.Track.CatalogMBID != "" && ac.Identity.MBID == ac.Track.CatalogMBID
}

func (ac *AcquisitionContext) fingerprintProvesReference() bool {
	if ac.Verdict.Kind != VerdictHard || ac.Identity.ReferenceDoubted {
		return false
	}
	return slices.ContainsFunc(ac.Verdict.Surviving, func(recording ports.LinkedRecording) bool {
		return recording.MBID != "" && slices.Contains(ac.Identity.MBIDs, recording.MBID)
	})
}
