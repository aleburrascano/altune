package service

import (
	"altune/go-api/internal/download/internal/core/audioverdict"
	"altune/go-api/internal/download/internal/core/ports"
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
	Judged    audioverdict.Verdict
}

func AudioVerdictOf(judged audioverdict.Verdict, results []ports.AcoustIDResult) AudioVerdict {
	return AudioVerdict{Kind: verdictKindOf(judged.Kind), Score: topScore(results), Surviving: judged.Linked, Judged: judged}
}

func verdictKindOf(kind audioverdict.Kind) VerdictKind {
	switch kind {
	case audioverdict.Proven:
		return VerdictHard
	case audioverdict.Likely:
		return VerdictSoft
	case audioverdict.Contradicted:
		return VerdictContradicted
	case audioverdict.Unknown:
		return VerdictUnknown
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
