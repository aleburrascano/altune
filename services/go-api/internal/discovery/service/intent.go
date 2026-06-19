package service

import (
	"context"
	"log/slog"
	"math"
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
	best := findBestSplit(ctx, tokens, vocab)
	if best != nil {
		slog.DebugContext(ctx, "intent.detected",
			"artist", best.Artist,
			"track", best.Track,
			"confidence", best.Confidence,
		)
	}
	return best
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

	matches, err := vocab.SuggestByPrefix(ctx, NormalizeForMatch(artistCandidate), 1)
	if err != nil || len(matches) == 0 {
		return
	}
	if matches[0].Kind != domain.VocabKindArtist {
		return
	}

	conf := math.Min(1.0, float64(matches[0].Popularity)/100.0)
	if *best == nil || conf > (*best).Confidence {
		*best = &QueryIntent{
			Artist:     artistCandidate,
			Track:      trackCandidate,
			Confidence: conf,
		}
	}
}

// intentBoost is the relevance score bonus applied when a query matches the
// "artist track" intent pattern (e.g., "Kendrick Lamar Humble"). Tuned to
// lift the structured-match result above same-score competitors without
// overwhelming a strong popularity signal.
const intentBoost = 0.15

