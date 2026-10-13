package vocabulary

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"log/slog"
	"strings"
	"unicode/utf8"
)

const (
	correctionCandidates    = 5
	maxCorrectionQueryRunes = 100
	maxCorrectionTokens     = 8
)

func correctable(norm string) bool {
	return norm != "" && utf8.RuneCountInString(norm) <= maxCorrectionQueryRunes
}

type CorrectionService struct {
	vocab ports.VocabularyReader
}

func NewCorrectionService(vocab ports.VocabularyReader) *CorrectionService {
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
	norm := textnorm.NormalizeForMatch(query)
	if !correctable(norm) {
		return nil
	}
	return s.correctWholeQuery(ctx, norm)
}

func (s *CorrectionService) CorrectAggressive(ctx context.Context, query string) *CorrectionResult {
	if s.vocab == nil {
		return nil
	}
	norm := textnorm.NormalizeForMatch(query)
	if !correctable(norm) {
		return nil
	}
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
	return pickBestCorrection(ctx, norm, candidates)
}

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
	if len(tokens) < 2 || len(tokens) > maxCorrectionTokens {
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

		matches, err := s.vocab.SuggestByPrefix(ctx, token, 1)
		if err != nil {
			slog.DebugContext(ctx, "correction.prefix_lookup_failed", "token", token, "error", err)
		}
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

		best := pickBestCorrection(ctx, token, candidates)
		if best == nil {
			corrected[i] = token
			continue
		}

		slog.DebugContext(ctx, "correction.token",
			logging.SearchTextAttr(token),
			slog.Group("corrected", logging.SearchTextAttr(best.Corrected)),
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

func pickBestCorrection(ctx context.Context, queryNorm string, candidates []domain.VocabularyEntry) *CorrectionResult {
	var best *CorrectionResult
	bestDist := len([]rune(queryNorm)) + 1
	maxDist := maxCorrectionDist(queryNorm)

	for _, c := range candidates {
		if c.TermNorm == queryNorm || textnorm.NormalizeForMatch(c.Term) == queryNorm {
			continue
		}
		dist := textnorm.LevenshteinDistance(queryNorm, c.TermNorm)
		logCandidate(ctx, queryNorm, c, dist)
		if dist <= maxDist && (dist < bestDist || (dist == bestDist && best != nil && c.MatchScore > best.Confidence)) {
			best = &CorrectionResult{
				Corrected:  c.Term,
				Confidence: c.MatchScore,
			}
			bestDist = dist
		}
	}
	return best
}

func logCandidate(ctx context.Context, queryNorm string, c domain.VocabularyEntry, dist int) {
	slog.DebugContext(ctx, "correction.candidate",
		logging.SearchTextAttr(queryNorm),
		slog.Group("candidate", logging.SearchTextAttr(c.TermNorm)),
		"distance", dist,
		"score", c.MatchScore,
	)
}

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
