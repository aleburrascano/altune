package service

import (
	"altune/go-api/internal/download/internal/ports"
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
	Judged    Verdict
}

func AudioVerdictOf(judged Verdict, results []ports.AcoustIDResult) AudioVerdict {
	return AudioVerdict{Kind: verdictKindOf(judged.Kind), Score: topScore(results), Surviving: judged.Linked, Judged: judged}
}

func verdictKindOf(kind JudgedKind) VerdictKind {
	switch kind {
	case JudgedProven:
		return VerdictHard
	case JudgedLikely:
		return VerdictSoft
	case JudgedContradicted:
		return VerdictContradicted
	case JudgedUnknown:
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
