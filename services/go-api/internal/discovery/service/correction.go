package service

import (
	"context"
	"log/slog"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

const correctionCandidates = 5
const correctionMinConfidence = 0.6

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

// Correct tries whole-query correction only. Used in the pre-correction
// phase before provider search, where false positives are costly.
func (s *CorrectionService) Correct(ctx context.Context, query string) *CorrectionResult {
	if s.vocab == nil {
		return nil
	}
	return s.correctWholeQuery(ctx, NormalizeForMatch(query))
}

// CorrectAggressive tries whole-query correction first, then falls back
// to token-level correction. Used in the post-correction phase when
// providers returned zero results and we need deeper recovery.
func (s *CorrectionService) CorrectAggressive(ctx context.Context, query string) *CorrectionResult {
	if s.vocab == nil {
		return nil
	}
	norm := NormalizeForMatch(query)
	if result := s.correctWholeQuery(ctx, norm); result != nil {
		return result
	}
	return s.correctTokens(ctx, norm)
}

func (s *CorrectionService) correctWholeQuery(ctx context.Context, norm string) *CorrectionResult {
	candidates, err := s.vocab.FindClosest(ctx, norm, correctionCandidates)
	if err != nil || len(candidates) == 0 {
		return nil
	}
	for _, c := range candidates {
		if c.TermNorm == norm && c.Kind != "query" {
			return nil
		}
	}
	return pickBestCorrection(norm, candidates)
}

func (s *CorrectionService) correctTokens(ctx context.Context, queryNorm string) *CorrectionResult {
	tokens := strings.Fields(queryNorm)
	if len(tokens) < 2 {
		return nil
	}

	corrected := make([]string, len(tokens))
	anyChanged := false
	minScore := 1.0

	for i, token := range tokens {
		if len(token) < 2 {
			corrected[i] = token
			continue
		}

		matches, _ := s.vocab.SuggestByPrefix(ctx, token, 1)
		if len(matches) > 0 && NormalizeForMatch(matches[0].Term) == token {
			corrected[i] = token
			continue
		}

		candidates, err := s.vocab.FindClosest(ctx, token, correctionCandidates)
		if err != nil || len(candidates) == 0 {
			corrected[i] = token
			continue
		}

		exactMatch := false
		for _, c := range candidates {
			if c.TermNorm == token && c.Kind != "query" {
				exactMatch = true
				break
			}
		}
		if exactMatch {
			corrected[i] = token
			continue
		}

		best := pickBestCorrection(token, candidates)
		if best == nil {
			corrected[i] = token
			continue
		}

		slog.Debug("correction.token",
			"original", token,
			"corrected", best.Corrected,
			"confidence", best.Confidence,
		)
		corrected[i] = NormalizeForMatch(best.Corrected)
		anyChanged = true
		if best.Confidence < minScore {
			minScore = best.Confidence
		}
	}

	if !anyChanged {
		return nil
	}

	return &CorrectionResult{
		Corrected:  strings.Join(corrected, " "),
		Confidence: minScore,
	}
}

func pickBestCorrection(queryNorm string, candidates []domain.VocabularyEntry) *CorrectionResult {
	var best *CorrectionResult
	for _, c := range candidates {
		if c.TermNorm == queryNorm || NormalizeForMatch(c.Term) == queryNorm {
			continue
		}
		score := c.MatchScore
		slog.Debug("correction.candidate",
			"query", queryNorm,
			"candidate", c.TermNorm,
			"score", score,
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
