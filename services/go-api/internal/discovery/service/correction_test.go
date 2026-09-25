package service

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestCorrectTokens_PrefixLookupErrorDegrades(t *testing.T) {
	store := &fakeVocabularyStore{
		suggestByPrefixFn: func(_ string, _ int) ([]domain.VocabularyEntry, error) {
			return nil, errors.New("redis down")
		},
		findClosestFn: func(token string, _ int) ([]domain.VocabularyEntry, error) {
			if token == "humbel" {
				return []domain.VocabularyEntry{{Term: "humble", TermNorm: "humble", Kind: domain.VocabKindTrack, MatchScore: 0.8}}, nil
			}
			return nil, nil
		},
	}
	svc := NewCorrectionService(store)

	result := svc.CorrectAggressive(context.Background(), "kendrick humbel")
	if result == nil || result.Corrected != "kendrick humble" {
		t.Fatalf("want token correction to survive the prefix-lookup error, got %+v", result)
	}
}

func TestPickBestCorrection(t *testing.T) {
	t.Run("picks closest by edit distance", func(t *testing.T) {
		candidates := []domain.VocabularyEntry{
			{Term: "megaman", TermNorm: "megaman", Kind: "track", MatchScore: 0.72},
			{Term: "madonna", TermNorm: "madonna", Kind: "artist", MatchScore: 0.30},
		}
		result := pickBestCorrection("megamsn", candidates)
		if result == nil {
			t.Fatal("expected a correction result")
		}
		if result.Corrected != "megaman" {
			t.Errorf("expected corrected='megaman', got %q", result.Corrected)
		}
	})

	t.Run("rejects candidates beyond max edit distance", func(t *testing.T) {
		candidates := []domain.VocabularyEntry{
			{Term: "zzzzzzz", TermNorm: "zzzzzzz", Kind: "track", MatchScore: 0.80},
			{Term: "yyyyyyy", TermNorm: "yyyyyyy", Kind: "track", MatchScore: 0.90},
		}
		result := pickBestCorrection("megamsn", candidates)
		if result != nil {
			t.Errorf("expected nil for high-distance candidates, got %+v", result)
		}
	})

	t.Run("empty candidates returns nil", func(t *testing.T) {
		result := pickBestCorrection("megamsn", nil)
		if result != nil {
			t.Errorf("expected nil, got %+v", result)
		}
	})

	t.Run("same distance breaks tie by match score", func(t *testing.T) {
		candidates := []domain.VocabularyEntry{
			{Term: "weekand", TermNorm: "weekand", Kind: "track", MatchScore: 0.45},
			{Term: "weekynd", TermNorm: "weekynd", Kind: "artist", MatchScore: 0.66},
		}
		result := pickBestCorrection("weekend", candidates)
		if result == nil {
			t.Fatal("expected a correction result")
		}
		if result.Corrected != "weekynd" {
			t.Errorf("expected higher-scored tiebreaker 'weekynd', got %q", result.Corrected)
		}
	})

	t.Run("prefers lower edit distance over higher score", func(t *testing.T) {
		candidates := []domain.VocabularyEntry{
			{Term: "megaman", TermNorm: "megaman", Kind: "track", MatchScore: 0.50},
			{Term: "megazan", TermNorm: "megazan", Kind: "track", MatchScore: 0.90},
		}
		result := pickBestCorrection("megamsn", candidates)
		if result == nil {
			t.Fatal("expected a correction result")
		}
		if result.Corrected != "megaman" {
			t.Errorf("expected closer 'megaman' (dist 1) over 'megazan' (dist 2), got %q", result.Corrected)
		}
	})
}

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

func TestPickBestCorrection_DoesNotLogRawQueryText(t *testing.T) {
	buf := captureLogs(t)

	store := &fakeVocabularyStore{
		findClosestFn: func(_ string, _ int) ([]domain.VocabularyEntry, error) {
			return []domain.VocabularyEntry{
				{Term: "humble", TermNorm: "humble", Kind: domain.VocabKindTrack, MatchScore: 0.8},
			}, nil
		},
	}
	svc := NewCorrectionService(store)

	result := svc.CorrectAggressive(context.Background(), "humbel")
	if result == nil || result.Corrected != "humble" {
		t.Fatalf("want whole-query correction, got %+v", result)
	}

	event := loggedEvent(t, buf, "correction.candidate")
	if strings.Contains(event, "humbel") {
		t.Fatalf("candidate log leaks raw query text:\n%s", event)
	}
	if !strings.Contains(event, `"fp"`) {
		t.Fatalf("expected a search-text fingerprint in place of the raw query:\n%s", event)
	}
}

func TestCorrection_NilVocabIsNil(t *testing.T) {
	s := NewCorrectionService(nil)
	if s.Correct(context.Background(), "humble") != nil {
		t.Error("Correct with no vocab must be nil")
	}
	if s.CorrectAggressive(context.Background(), "humble") != nil {
		t.Error("CorrectAggressive with no vocab must be nil")
	}
}

func TestCorrection_CorrectWholeQuery(t *testing.T) {
	store := &fakeVocabularyStore{
		findClosestFn: func(query string, _ int) ([]domain.VocabularyEntry, error) {
			return []domain.VocabularyEntry{
				{Term: "Kendrick", TermNorm: "kendrick", Kind: domain.VocabKindArtist, MatchScore: 0.9},
			}, nil
		},
	}
	s := NewCorrectionService(store)

	got := s.Correct(context.Background(), "kendrik")
	if got == nil || got.Corrected != "Kendrick" {
		t.Fatalf("Correct = %+v, want the distance-1 vocab term", got)
	}

	if got := s.Correct(context.Background(), "kendrick"); got != nil {
		t.Errorf("exact vocab term corrected to %+v, want nil", got)
	}
}

func TestCorrection_WholeQueryErrorOrEmptyIsNil(t *testing.T) {
	erroring := &fakeVocabularyStore{
		findClosestFn: func(string, int) ([]domain.VocabularyEntry, error) {
			return nil, errors.New("redis down")
		},
	}
	if got := NewCorrectionService(erroring).Correct(context.Background(), "kendrik"); got != nil {
		t.Errorf("store error must degrade to nil, got %+v", got)
	}
	empty := &fakeVocabularyStore{}
	if got := NewCorrectionService(empty).Correct(context.Background(), "kendrik"); got != nil {
		t.Errorf("no candidates must yield nil, got %+v", got)
	}
}

func TestMaxCorrectionDist_Boundaries(t *testing.T) {
	tests := []struct {
		query string
		want  int
	}{
		{"abcd", 1},
		{"abcde", 2},
		{"abcdefgh", 2},
		{"abcdefghi", 3},
	}
	for _, tt := range tests {
		if got := maxCorrectionDist(tt.query); got != tt.want {
			t.Errorf("maxCorrectionDist(%q) = %d, want %d", tt.query, got, tt.want)
		}
	}
}

func TestCorrection_TokenPathSingleTokenIsNil(t *testing.T) {
	store := &fakeVocabularyStore{}
	s := NewCorrectionService(store)
	if got := s.CorrectAggressive(context.Background(), "kendrik"); got != nil {
		t.Errorf("single-token aggressive miss = %+v, want nil", got)
	}
}

func TestCorrection_TokenPathPrefixErrorDegrades(t *testing.T) {
	store := &fakeVocabularyStore{
		suggestByPrefixFn: func(string, int) ([]domain.VocabularyEntry, error) {
			return nil, errors.New("redis down")
		},
		findClosestFn: func(query string, _ int) ([]domain.VocabularyEntry, error) {
			if query == "kendrik lamar" {
				return nil, nil
			}
			if query == "kendrik" {
				return []domain.VocabularyEntry{{Term: "Kendrick", TermNorm: "kendrick", Kind: domain.VocabKindArtist, MatchScore: 0.8}}, nil
			}
			return []domain.VocabularyEntry{{Term: "Lamar", TermNorm: "lamar", Kind: domain.VocabKindArtist, MatchScore: 1}}, nil
		},
	}
	s := NewCorrectionService(store)
	got := s.CorrectAggressive(context.Background(), "kendrik lamar")
	if got == nil || got.Corrected != "kendrick lamar" {
		t.Fatalf("aggressive = %+v, want token-corrected \"kendrick lamar\"", got)
	}
	if got.Confidence != 0.8 {
		t.Errorf("confidence = %v, want the min corrected-token score 0.8", got.Confidence)
	}
}
