package service

import (
	"context"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type QueryIntent struct {
	Artist     string
	Track      string
	Confidence float64
}

func DetectIntent(ctx context.Context, query string, vocab ports.VocabularyStore) *QueryIntent {
	if vocab == nil {
		return nil
	}
	tokens := strings.Fields(strings.TrimSpace(query))
	if len(tokens) < 2 {
		return nil
	}
	return findBestSplit(ctx, tokens, vocab)
}

func findBestSplit(ctx context.Context, tokens []string, vocab ports.VocabularyStore) *QueryIntent {
	var best *QueryIntent
	for i := 1; i < len(tokens); i++ {
		checkSplit(ctx, tokens[:i], tokens[i:], vocab, &best)
		checkSplit(ctx, tokens[i:], tokens[:i], vocab, &best)
	}
	return best
}

func checkSplit(
	ctx context.Context,
	artistTokens, trackTokens []string,
	vocab ports.VocabularyStore,
	best **QueryIntent,
) {
	artistCandidate := strings.Join(artistTokens, " ")
	trackCandidate := strings.Join(trackTokens, " ")
	if trackCandidate == "" {
		return
	}

	matches, err := vocab.SuggestByPrefix(ctx, strings.ToLower(artistCandidate), 1)
	if err != nil || len(matches) == 0 {
		return
	}
	if matches[0].Kind != "artist" {
		return
	}

	conf := float64(matches[0].Popularity) / 100.0
	if *best == nil || conf > (*best).Confidence {
		*best = &QueryIntent{
			Artist:     artistCandidate,
			Track:      trackCandidate,
			Confidence: conf,
		}
	}
}

const intentBoost = 0.15

func ApplyIntentBoost(results []domain.SearchResult, intent *QueryIntent) []domain.SearchResult {
	if intent == nil {
		return results
	}
	artistNorm := NormalizeForMatch(intent.Artist)
	trackNorm := NormalizeForMatch(intent.Track)
	for i, r := range results {
		results[i] = boostIfIntentMatch(r, artistNorm, trackNorm)
	}
	return results
}

func boostIfIntentMatch(r domain.SearchResult, artistNorm, trackNorm string) domain.SearchResult {
	subtitleNorm := NormalizeForMatch(r.Subtitle)
	titleNorm := NormalizeForMatch(r.Title)
	artistMatch := strings.Contains(subtitleNorm, artistNorm)
	trackMatch := strings.Contains(titleNorm, trackNorm)
	if !artistMatch || !trackMatch {
		return r
	}
	pop := popularity(r)
	boosted := pop + intentBoost*100
	if boosted > 100 {
		boosted = 100
	}
	extras := copyExtras(r.Extras)
	extras["popularity"] = int64(boosted)
	r.Extras = extras
	return r
}
