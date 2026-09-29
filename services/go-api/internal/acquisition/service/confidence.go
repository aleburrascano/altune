package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"log/slog"
	"math"
)

const (
	defaultConfidenceFloor = 0.5
	confidenceScale        = 100

	hardConfidence        = 0.95
	softConfidence        = 0.85
	lowScoreCutoff        = 0.92
	lowScorePenalty       = 0.10
	unknownBase           = 0.2
	unknownCap            = 0.8
	trustedChannelBonus   = 0.3
	tightDurationBonus    = 0.2
	looseDurationBonus    = 0.1
	resolvedBonus         = 0.1
	qualifierPenalty      = 0.2
	tightDurationSeconds  = 2.0
	looseDurationSeconds  = 15.0
	looseDurationFraction = 0.07

	channelTopic  = "topic"
	channelArtist = "artist"
	channelOther  = "other"

	fallbackDurationSeconds  = 5.0
	fallbackDurationFraction = 0.03
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

func durationBonus(delta, trackSeconds float64) float64 {
	delta = math.Abs(delta)
	switch {
	case delta <= tightDurationSeconds:
		return tightDurationBonus
	case delta <= math.Max(looseDurationSeconds, looseDurationFraction*trackSeconds):
		return looseDurationBonus
	default:
		return 0
	}
}

func channelClass(trackArtist, channel string) string {
	switch {
	case isTopicChannel(channel):
		return channelTopic
	case artistMatchesChannel(trackArtist, channel):
		return channelArtist
	default:
		return channelOther
	}
}

func (ac *AcquisitionContext) buildEvidence(candidate ports.AudioCandidate, verified verificationResult) Evidence {
	_, fallback := UnrequestedQualifiers(ac.Track.Title, ac.Track.Artist, candidate.Title)
	return Evidence{
		Verdict:              string(verified.verdict.Kind),
		AcoustIDScore:        verified.verdict.Score,
		DurationDeltaSeconds: ac.durationDelta(candidate, verified.probed),
		Channel:              channelClass(ac.Track.Artist, candidate.Channel),
		Qualifiers:           fallback,
		Resolved:             candidate.Resolved,
		SourceTitle:          candidate.Title,
	}
}

func (ac *AcquisitionContext) durationDelta(candidate ports.AudioCandidate, probed float64) float64 {
	measured := probed
	if measured <= 0 {
		measured = candidate.Duration
	}
	expected := ac.Identity.Duration
	if expected <= 0 {
		expected = ac.Track.Duration
	}
	if measured <= 0 || expected <= 0 {
		return 0
	}
	return measured - expected
}

func fallbackLengthMatches(delta, trackSeconds float64) bool {
	return math.Abs(delta) <= math.Max(fallbackDurationSeconds, fallbackDurationFraction*trackSeconds)
}

func (ac *AcquisitionContext) adopt(ctx context.Context, evidence Evidence) {
	ac.Evidence = evidence
	ac.Confidence = ScoreConfidence(evidence, ac.Track.Duration)
	slog.InfoContext(ctx, "acquisition.confidence",
		"track_id", ac.Track.ID,
		"confidence", ac.Confidence,
		"evidence", evidence,
	)
}
