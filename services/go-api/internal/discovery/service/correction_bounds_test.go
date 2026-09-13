package service

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"strings"
	"sync"
	"testing"
)

// ctxRecordingVocab records how many fuzzy lookups ran and whether each ran
// under a deadline.
type ctxRecordingVocab struct {
	mu               sync.Mutex
	findClosestCalls int
	queries          []string
	sawNoDeadline    bool
}

func (v *ctxRecordingVocab) record(ctx context.Context) {
	if _, ok := ctx.Deadline(); !ok {
		v.sawNoDeadline = true
	}
}

func (v *ctxRecordingVocab) SuggestByPrefix(ctx context.Context, _ string, _ int) ([]domain.VocabularyEntry, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.record(ctx)
	return nil, nil
}

func (v *ctxRecordingVocab) FindClosest(ctx context.Context, query string, _ int) ([]domain.VocabularyEntry, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.record(ctx)
	v.findClosestCalls++
	v.queries = append(v.queries, query)
	return nil, nil
}

func TestCorrectAggressive_ManyTokenQueryIsBounded(t *testing.T) {
	vocab := &ctxRecordingVocab{}
	svc := NewCorrectionService(vocab)

	hostile := strings.TrimSpace(strings.Repeat("zxqv ", 5000))
	svc.CorrectAggressive(context.Background(), hostile)

	// one whole-query lookup plus at most maxCorrectionTokens per-token lookups
	if limit := 1 + maxCorrectionTokens; vocab.findClosestCalls > limit {
		t.Errorf("FindClosest calls = %d, want at most %d", vocab.findClosestCalls, limit)
	}
	for _, q := range vocab.queries {
		if n := len([]rune(q)); n > maxCorrectionQueryRunes {
			t.Errorf("fuzzy lookup received a %d-rune query, want at most %d", n, maxCorrectionQueryRunes)
		}
	}
}

func TestCorrectAggressive_ShortMultiTokenQueryStillCorrectsEveryToken(t *testing.T) {
	vocab := &ctxRecordingVocab{}
	svc := NewCorrectionService(vocab)

	svc.CorrectAggressive(context.Background(), "kendrik lamar humbel")

	if vocab.findClosestCalls != 4 {
		t.Errorf("FindClosest calls = %d, want 4 (whole query + 3 tokens)", vocab.findClosestCalls)
	}
}

func TestTryCorrection_RunsUnderExplicitDeadline(t *testing.T) {
	vocab := &ctxRecordingVocab{}
	s := &Service{correctionSvc: NewCorrectionService(vocab)}
	query, err := domain.NewSearchQuery("kendrik lamar", map[domain.ResultKind]bool{domain.ResultKindArtist: true}, 10)
	if err != nil {
		t.Fatalf("NewSearchQuery: %v", err)
	}

	s.tryCorrection(context.Background(), query)

	if vocab.findClosestCalls == 0 {
		t.Fatal("correction never consulted the vocabulary")
	}
	if vocab.sawNoDeadline {
		t.Error("vocabulary lookups ran on a context with no deadline")
	}
}
