package vocabulary_test

import (
	"altune/go-api/internal/discovery"
	"context"
	"errors"
	"strings"
	"testing"
)

const (
	maxCorrectionQueryRunes = 100
	maxCorrectionTokens     = 8
)

func vocabOffering(candidates ...discovery.VocabularyEntry) *discovery.FakeVocabularyStore {
	return &discovery.FakeVocabularyStore{
		FindClosestFn: func(string, int) ([]discovery.VocabularyEntry, error) {
			return candidates, nil
		},
	}
}

func trackTerm(term string, score float64) discovery.VocabularyEntry {
	return discovery.VocabularyEntry{Term: term, TermNorm: term, Kind: discovery.VocabKindTrack, MatchScore: score}
}

func correctedTo(t *testing.T, store *discovery.FakeVocabularyStore, query string) string {
	t.Helper()
	result := discovery.NewCorrectionService(store).Correct(context.Background(), query)
	if result == nil {
		return ""
	}
	return result.Corrected
}

func TestCorrectTokens_PrefixLookupErrorDegrades(t *testing.T) {
	store := &discovery.FakeVocabularyStore{
		SuggestByPrefixFn: func(_ string, _ int) ([]discovery.VocabularyEntry, error) {
			return nil, errors.New("redis down")
		},
		FindClosestFn: func(token string, _ int) ([]discovery.VocabularyEntry, error) {
			if token == "humbel" {
				return []discovery.VocabularyEntry{trackTerm("humble", 0.8)}, nil
			}
			return nil, nil
		},
	}
	svc := discovery.NewCorrectionService(store)

	result := svc.CorrectAggressive(context.Background(), "kendrick humbel")
	if result == nil || result.Corrected != "kendrick humble" {
		t.Fatalf("want token correction to survive the prefix-lookup error, got %+v", result)
	}
}

func TestCorrect_PicksTheBestCandidate(t *testing.T) {
	cases := []struct {
		name       string
		query      string
		candidates []discovery.VocabularyEntry
		want       string
	}{
		{
			name:       "closest by edit distance",
			query:      "megamsn",
			candidates: []discovery.VocabularyEntry{trackTerm("megaman", 0.72), {Term: "madonna", TermNorm: "madonna", Kind: discovery.VocabKindArtist, MatchScore: 0.30}},
			want:       "megaman",
		},
		{
			name:       "nothing when every candidate is beyond the max edit distance",
			query:      "megamsn",
			candidates: []discovery.VocabularyEntry{trackTerm("zzzzzzz", 0.80), trackTerm("yyyyyyy", 0.90)},
			want:       "",
		},
		{
			name:       "a lone candidate",
			query:      "megamsn",
			candidates: []discovery.VocabularyEntry{trackTerm("megaman", 0.72)},
			want:       "megaman",
		},
		{
			name:       "same distance breaks the tie by match score",
			query:      "weekend",
			candidates: []discovery.VocabularyEntry{trackTerm("weekand", 0.45), {Term: "weekynd", TermNorm: "weekynd", Kind: discovery.VocabKindArtist, MatchScore: 0.66}},
			want:       "weekynd",
		},
		{
			name:       "same distance keeps the first when the later scores no higher",
			query:      "weekend",
			candidates: []discovery.VocabularyEntry{trackTerm("weekand", 0.66), trackTerm("weekynd", 0.66)},
			want:       "weekand",
		},
		{
			name:       "lower edit distance beats a higher score",
			query:      "megamsn",
			candidates: []discovery.VocabularyEntry{trackTerm("megaman", 0.50), trackTerm("megazan", 0.90)},
			want:       "megaman",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := correctedTo(t, vocabOffering(c.candidates...), c.query); got != c.want {
				t.Errorf("Correct(%q) = %q, want %q", c.query, got, c.want)
			}
		})
	}
}

func TestCorrect_MaxEditDistanceGrowsWithQueryLength(t *testing.T) {
	cases := []struct {
		query, withinReach, outOfReach string
	}{
		{"abcd", "abcx", "abxx"},
		{"abcde", "abcxx", "abxxx"},
		{"abcdefgh", "abcdefxx", "abcdexxx"},
		{"abcdefghi", "abcdefxxx", "abcdexxxx"},
	}
	for _, c := range cases {
		t.Run(c.query, func(t *testing.T) {
			if got := correctedTo(t, vocabOffering(trackTerm(c.withinReach, 0.5)), c.query); got != c.withinReach {
				t.Errorf("Correct(%q) with %q = %q, want it corrected", c.query, c.withinReach, got)
			}
			if got := correctedTo(t, vocabOffering(trackTerm(c.outOfReach, 0.5)), c.query); got != "" {
				t.Errorf("Correct(%q) with %q = %q, want no correction", c.query, c.outOfReach, got)
			}
		})
	}
}

func TestCorrectAggressive_ManyTokenQueryIsBounded(t *testing.T) {
	vocab := &discovery.CtxRecordingVocab{}
	svc := discovery.NewCorrectionService(vocab)

	hostile := strings.TrimSpace(strings.Repeat("zxqv ", 5000))
	svc.CorrectAggressive(context.Background(), hostile)

	if limit := 1 + maxCorrectionTokens; vocab.FindClosestCalls > limit {
		t.Errorf("FindClosest calls = %d, want at most %d", vocab.FindClosestCalls, limit)
	}
	for _, q := range vocab.Queries {
		if n := len([]rune(q)); n > maxCorrectionQueryRunes {
			t.Errorf("fuzzy lookup received a %d-rune query, want at most %d", n, maxCorrectionQueryRunes)
		}
	}
}

func TestCorrectAggressive_ShortMultiTokenQueryStillCorrectsEveryToken(t *testing.T) {
	vocab := &discovery.CtxRecordingVocab{}
	svc := discovery.NewCorrectionService(vocab)

	svc.CorrectAggressive(context.Background(), "kendrik lamar humbel")

	if vocab.FindClosestCalls != 4 {
		t.Errorf("FindClosest calls = %d, want 4 (whole query + 3 tokens)", vocab.FindClosestCalls)
	}
}

func TestCorrect_DoesNotLogRawQueryText(t *testing.T) {
	buf := discovery.CaptureLogs(t)

	svc := discovery.NewCorrectionService(vocabOffering(trackTerm("humble", 0.8)))

	result := svc.CorrectAggressive(context.Background(), "humbel")
	if result == nil || result.Corrected != "humble" {
		t.Fatalf("want whole-query correction, got %+v", result)
	}

	event := discovery.LoggedEvent(t, buf, "correction.candidate")
	if strings.Contains(event, "humbel") {
		t.Fatalf("candidate log leaks raw query text:\n%s", event)
	}
	if !strings.Contains(event, `"fp"`) {
		t.Fatalf("expected a search-text fingerprint in place of the raw query:\n%s", event)
	}
}

func TestCorrection_NilVocabIsNil(t *testing.T) {
	s := discovery.NewCorrectionService(nil)
	if s.Correct(context.Background(), "humble") != nil {
		t.Error("Correct with no vocab must be nil")
	}
	if s.CorrectAggressive(context.Background(), "humble") != nil {
		t.Error("CorrectAggressive with no vocab must be nil")
	}
}

func TestCorrection_CorrectWholeQuery(t *testing.T) {
	store := vocabOffering(discovery.VocabularyEntry{Term: "Kendrick", TermNorm: "kendrick", Kind: discovery.VocabKindArtist, MatchScore: 0.9})
	s := discovery.NewCorrectionService(store)

	got := s.Correct(context.Background(), "kendrik")
	if got == nil || got.Corrected != "Kendrick" {
		t.Fatalf("Correct = %+v, want the distance-1 vocab term", got)
	}

	if got := s.Correct(context.Background(), "kendrick"); got != nil {
		t.Errorf("exact vocab term corrected to %+v, want nil", got)
	}
}

func TestCorrection_WholeQueryErrorOrEmptyIsNil(t *testing.T) {
	erroring := &discovery.FakeVocabularyStore{
		FindClosestFn: func(string, int) ([]discovery.VocabularyEntry, error) {
			return nil, errors.New("redis down")
		},
	}
	if got := discovery.NewCorrectionService(erroring).Correct(context.Background(), "kendrik"); got != nil {
		t.Errorf("store error must degrade to nil, got %+v", got)
	}
	empty := &discovery.FakeVocabularyStore{}
	if got := discovery.NewCorrectionService(empty).Correct(context.Background(), "kendrik"); got != nil {
		t.Errorf("no candidates must yield nil, got %+v", got)
	}
}

func TestCorrection_TokenPathSingleTokenIsNil(t *testing.T) {
	s := discovery.NewCorrectionService(&discovery.FakeVocabularyStore{})
	if got := s.CorrectAggressive(context.Background(), "kendrik"); got != nil {
		t.Errorf("single-token aggressive miss = %+v, want nil", got)
	}
}

func TestCorrection_TokenPathPrefixErrorDegrades(t *testing.T) {
	store := &discovery.FakeVocabularyStore{
		SuggestByPrefixFn: func(string, int) ([]discovery.VocabularyEntry, error) {
			return nil, errors.New("redis down")
		},
		FindClosestFn: func(query string, _ int) ([]discovery.VocabularyEntry, error) {
			if query == "kendrik lamar" {
				return nil, nil
			}
			if query == "kendrik" {
				return []discovery.VocabularyEntry{{Term: "Kendrick", TermNorm: "kendrick", Kind: discovery.VocabKindArtist, MatchScore: 0.8}}, nil
			}
			return []discovery.VocabularyEntry{{Term: "Lamar", TermNorm: "lamar", Kind: discovery.VocabKindArtist, MatchScore: 1}}, nil
		},
	}
	s := discovery.NewCorrectionService(store)
	got := s.CorrectAggressive(context.Background(), "kendrik lamar")
	if got == nil || got.Corrected != "kendrick lamar" {
		t.Fatalf("aggressive = %+v, want token-corrected \"kendrick lamar\"", got)
	}
	if got.Confidence != 0.8 {
		t.Errorf("confidence = %v, want the min corrected-token score 0.8", got.Confidence)
	}
}
