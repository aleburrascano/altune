package service

import (
	"context"
	"log/slog"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

const correctionCandidates = 5

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
	return s.correctWholeQuery(ctx, textnorm.NormalizeForMatch(query))
}

// CorrectAggressive tries whole-query correction first, then falls back
// to token-level correction. Used in the post-correction phase when
// providers returned zero results and we need deeper recovery.
func (s *CorrectionService) CorrectAggressive(ctx context.Context, query string) *CorrectionResult {
	if s.vocab == nil {
		return nil
	}
	norm := textnorm.NormalizeForMatch(query)
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
	if isExactVocabMatch(candidates, norm) {
		return nil
	}
	return pickBestCorrection(norm, candidates)
}

// isExactVocabMatch reports whether any candidate is an exact non-query vocabulary
// match for norm — meaning the input is already a confirmed entity term and should
// not be corrected.
func isExactVocabMatch(candidates []domain.VocabularyEntry, norm string) bool {
	for _, c := range candidates {
		if c.TermNorm == norm && c.Kind != domain.VocabKindQuery {
			return true
		}
	}
	return false
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
		if len(matches) > 0 && textnorm.NormalizeForMatch(matches[0].Term) == token {
			corrected[i] = token
			continue
		}

		candidates, err := s.vocab.FindClosest(ctx, token, correctionCandidates)
		if err != nil || len(candidates) == 0 {
			corrected[i] = token
			continue
		}

		if isExactVocabMatch(candidates, token) {
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
		corrected[i] = textnorm.NormalizeForMatch(best.Corrected)
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
	bestDist := len([]rune(queryNorm)) + 1
	maxDist := maxCorrectionDist(queryNorm)

	for _, c := range candidates {
		if c.TermNorm == queryNorm || textnorm.NormalizeForMatch(c.Term) == queryNorm {
			continue
		}
		dist := textnorm.LevenshteinDistance(queryNorm, c.TermNorm)
		slog.Debug("correction.candidate",
			"query", queryNorm,
			"candidate", c.TermNorm,
			"distance", dist,
			"score", c.MatchScore,
		)
		if dist > maxDist {
			continue
		}
		if dist < bestDist || (dist == bestDist && best != nil && c.MatchScore > best.Confidence) {
			best = &CorrectionResult{
				Corrected:  c.Term,
				Confidence: c.MatchScore,
			}
			bestDist = dist
		}
	}
	return best
}

// maxCorrectionDist returns the maximum edit distance allowed for a correction
// candidate. Short queries (<=4 runes) tolerate only 1 edit to avoid wild
// corrections; medium queries (<=8) tolerate 2; longer queries tolerate 3.
func maxCorrectionDist(query string) int {
	n := len([]rune(query))
	if n <= 4 {
		return 1
	}
	if n <= 8 {
		return 2
	}
	return 3
}
