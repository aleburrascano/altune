package service

import (
	"context"
	"log/slog"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

const correctionCandidates = 5
const correctionMinConfidence = 0.4

type CorrectionService struct {
	vocab ports.VocabularyStore
}

func NewCorrectionService(vocab ports.VocabularyStore) *CorrectionService {
	return &CorrectionService{vocab: vocab}
}

type CorrectionResult struct {
	Corrected  string
	Confidence float64
}

func (s *CorrectionService) Correct(ctx context.Context, query string) *CorrectionResult {
	if s.vocab == nil {
		return nil
	}
	norm := NormalizeForMatch(query)
	candidates, err := s.vocab.FindClosest(ctx, norm, correctionCandidates)
	if err != nil || len(candidates) == 0 {
		return nil
	}
	return pickBestCorrection(norm, candidates)
}

func pickBestCorrection(queryNorm string, candidates []domain.VocabularyEntry) *CorrectionResult {
	var best *CorrectionResult
	for _, c := range candidates {
		score := trigramJaccard(queryNorm, c.TermNorm)
		slog.Debug("correction.candidate",
			"query", queryNorm,
			"candidate", c.TermNorm,
			"jaccard", score,
		)
		if score < correctionMinConfidence {
			continue
		}
		if best == nil || score > best.Confidence {
			best = &CorrectionResult{
				Corrected:  c.Term,
				Confidence: score,
			}
		}
	}
	return best
}

func trigramJaccard(a, b string) float64 {
	tA := trigrams(a)
	tB := trigrams(b)
	if len(tA) == 0 || len(tB) == 0 {
		return 0
	}
	shared := 0
	for tri := range tA {
		if tB[tri] {
			shared++
		}
	}
	union := len(tA) + len(tB) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

func trigrams(s string) map[string]bool {
	if len(s) < 3 {
		return map[string]bool{s: true}
	}
	result := make(map[string]bool)
	runes := []rune(s)
	for i := 0; i <= len(runes)-3; i++ {
		result[string(runes[i:i+3])] = true
	}
	return result
}
