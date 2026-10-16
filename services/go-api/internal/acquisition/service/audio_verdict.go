package service

import (
	"altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/audiojudge"
	"math"
)

type VerdictKind string

const (
	VerdictHard          VerdictKind = "hard"
	VerdictSoft          VerdictKind = "soft"
	VerdictOtherVersion  VerdictKind = "other_version"
	VerdictDifferentSong VerdictKind = "different_song"
	VerdictUnknown       VerdictKind = "unknown"
	VerdictContradicted  VerdictKind = "contradicted"
)

type AudioVerdict struct {
	Kind      VerdictKind
	Score     float64
	Surviving []ports.LinkedRecording
	Judged    audiojudge.Verdict
}

func audioVerdictOf(judged audiojudge.Verdict, results []ports.AcoustIDResult) AudioVerdict {
	return AudioVerdict{Kind: verdictKindOf(judged.Kind), Score: topScore(results), Surviving: judged.Linked, Judged: judged}
}

func verdictKindOf(kind audiojudge.Kind) VerdictKind {
	switch kind {
	case audiojudge.Proven:
		return VerdictHard
	case audiojudge.Likely:
		return VerdictSoft
	case audiojudge.Contradicted:
		return VerdictContradicted
	}
	return VerdictUnknown
}

func topScore(results []ports.AcoustIDResult) float64 {
	var best float64
	seen := false
	for _, result := range results {
		if math.IsNaN(result.Score) {
			continue
		}
		if !seen || result.Score > best {
			best = result.Score
			seen = true
		}
	}
	return best
}

func artistNames(credit string) map[string]bool {
	names := make(map[string]bool)
	for _, part := range domain.SplitCredit(credit) {
		if name := domain.CreditKey(part); name != "" {
			names[name] = true
		}
	}
	return names
}
