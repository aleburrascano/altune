package vocabulary_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	"context"
	"errors"
	"strings"
	"testing"
)

const (
	maxCorrectionQueryRunes = 100
	maxCorrectionTokens     = 8
)

func vocabOffering(candidates ...search.SpellingHintsEntry) *search.FakeVocabularyStore {
	return &search.FakeVocabularyStore{
		FindClosestFn: func(string, int) ([]search.SpellingHintsEntry, error) {
			return candidates, nil
		},
	}
}

func trackTerm(term string, score float64) search.SpellingHintsEntry {
	return search.SpellingHintsEntry{Term: term, TermNorm: term, Kind: search.VocabKindTrack, MatchScore: score}
}

func correctedTo(t *testing.T, store *search.FakeVocabularyStore, query string) string {
	t.Helper()
	result := searchtest.NewCorrection(t, store).Correct(context.Background(), query)
	if result == nil {
		return ""
	}
	return result.Corrected
}

func TestCorrectTokens_PrefixLookupErrorDegrades(t *testing.T) {
	store := &search.FakeVocabularyStore{
		SuggestByPrefixFn: func(_ string, _ int) ([]search.SpellingHintsEntry, error) {
			return nil, errors.New("redis down")
		},
		FindClosestFn: func(token string, _ int) ([]search.SpellingHintsEntry, error) {
			if token == "humbel" {
				return []search.SpellingHintsEntry{trackTerm("humble", 0.8)}, nil
			}
			return nil, nil
		},
	}
	svc := searchtest.NewCorrection(t, store)

	result := svc.CorrectAggressive(context.Background(), "kendrick humbel")
	if result == nil || result.Corrected != "kendrick humble" {
		t.Fatalf("want token correction to survive the prefix-lookup error, got %+v", result)
	}
}

func TestCorrect_PicksTheBestCandidate(t *testing.T) {
	cases := []struct {
		name       string
		query      string
		candidates []search.SpellingHintsEntry
		want       string
	}{
		{
			name:       "closest by edit distance",
			query:      "megamsn",
			candidates: []search.SpellingHintsEntry{trackTerm("megaman", 0.72), {Term: "madonna", TermNorm: "madonna", Kind: search.VocabKindArtist, MatchScore: 0.30}},
			want:       "megaman",
		},
		{
			name:       "nothing when every candidate is beyond the max edit distance",
			query:      "megamsn",
			candidates: []search.SpellingHintsEntry{trackTerm("zzzzzzz", 0.80), trackTerm("yyyyyyy", 0.90)},
			want:       "",
		},
		{
			name:       "a lone candidate",
			query:      "megamsn",
			candidates: []search.SpellingHintsEntry{trackTerm("megaman", 0.72)},
			want:       "megaman",
		},
		{
			name:       "same distance breaks the tie by match score",
			query:      "weekend",
			candidates: []search.SpellingHintsEntry{trackTerm("weekand", 0.45), {Term: "weekynd", TermNorm: "weekynd", Kind: search.VocabKindArtist, MatchScore: 0.66}},
			want:       "weekynd",
		},
		{
			name:       "same distance keeps the first when the later scores no higher",
			query:      "weekend",
			candidates: []search.SpellingHintsEntry{trackTerm("weekand", 0.66), trackTerm("weekynd", 0.66)},
			want:       "weekand",
		},
		{
			name:       "lower edit distance beats a higher score",
			query:      "megamsn",
			candidates: []search.SpellingHintsEntry{trackTerm("megaman", 0.50), trackTerm("megazan", 0.90)},
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
	vocab := &search.CtxRecordingVocab{}
	svc := searchtest.NewCorrection(t, vocab)

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
	vocab := &search.CtxRecordingVocab{}
	svc := searchtest.NewCorrection(t, vocab)

	svc.CorrectAggressive(context.Background(), "kendrik lamar humbel")

	if vocab.FindClosestCalls != 4 {
		t.Errorf("FindClosest calls = %d, want 4 (whole query + 3 tokens)", vocab.FindClosestCalls)
	}
}

func TestCorrect_DoesNotLogRawQueryText(t *testing.T) {
	buf := search.CaptureLogs(t)

	svc := searchtest.NewCorrection(t, vocabOffering(trackTerm("humble", 0.8)))

	result := svc.CorrectAggressive(context.Background(), "humbel")
	if result == nil || result.Corrected != "humble" {
		t.Fatalf("want whole-query correction, got %+v", result)
	}

	event := search.LoggedEvent(t, buf, "correction.candidate")
	if strings.Contains(event, "humbel") {
		t.Fatalf("candidate log leaks raw query text:\n%s", event)
	}
	if !strings.Contains(event, `"fp"`) {
		t.Fatalf("expected a search-text fingerprint in place of the raw query:\n%s", event)
	}
}

func TestCorrection_RefusesAMissingVocabulary(t *testing.T) {
	if _, err := search.NewCorrectionService(nil); !errors.Is(err, search.ErrVocabularyCollaboratorRequired) {
		t.Fatalf("err = %v, want ErrVocabularyCollaboratorRequired", err)
	}
}

func TestVocabularyIngestor_RefusesAMissingStoreOrTracker(t *testing.T) {
	bg := searchtest.NewTracker(t)
	for name, build := range map[string]func() error{
		"store":   func() error { _, err := search.NewVocabularyIngestor(nil, bg); return err },
		"tracker": func() error { _, err := search.NewVocabularyIngestor(&search.FakeVocabularyStore{}, nil); return err },
	} {
		if err := build(); !errors.Is(err, search.ErrVocabularyCollaboratorRequired) {
			t.Errorf("missing %s: err = %v, want ErrVocabularyCollaboratorRequired", name, err)
		}
	}
}

func TestCorrection_CorrectWholeQuery(t *testing.T) {
	store := vocabOffering(search.SpellingHintsEntry{Term: "Kendrick", TermNorm: "kendrick", Kind: search.VocabKindArtist, MatchScore: 0.9})
	s := searchtest.NewCorrection(t, store)

	got := s.Correct(context.Background(), "kendrik")
	if got == nil || got.Corrected != "Kendrick" {
		t.Fatalf("Correct = %+v, want the distance-1 vocab term", got)
	}

	if got := s.Correct(context.Background(), "kendrick"); got != nil {
		t.Errorf("exact vocab term corrected to %+v, want nil", got)
	}
}

func TestCorrection_WholeQueryErrorOrEmptyIsNil(t *testing.T) {
	erroring := &search.FakeVocabularyStore{
		FindClosestFn: func(string, int) ([]search.SpellingHintsEntry, error) {
			return nil, errors.New("redis down")
		},
	}
	if got := searchtest.NewCorrection(t, erroring).Correct(context.Background(), "kendrik"); got != nil {
		t.Errorf("store error must degrade to nil, got %+v", got)
	}
	empty := &search.FakeVocabularyStore{}
	if got := searchtest.NewCorrection(t, empty).Correct(context.Background(), "kendrik"); got != nil {
		t.Errorf("no candidates must yield nil, got %+v", got)
	}
}

func TestCorrection_TokenPathSingleTokenIsNil(t *testing.T) {
	s := searchtest.NewCorrection(t, &search.FakeVocabularyStore{})
	if got := s.CorrectAggressive(context.Background(), "kendrik"); got != nil {
		t.Errorf("single-token aggressive miss = %+v, want nil", got)
	}
}

func TestCorrection_TokenPathPrefixErrorDegrades(t *testing.T) {
	store := &search.FakeVocabularyStore{
		SuggestByPrefixFn: func(string, int) ([]search.SpellingHintsEntry, error) {
			return nil, errors.New("redis down")
		},
		FindClosestFn: func(query string, _ int) ([]search.SpellingHintsEntry, error) {
			if query == "kendrik lamar" {
				return nil, nil
			}
			if query == "kendrik" {
				return []search.SpellingHintsEntry{{Term: "Kendrick", TermNorm: "kendrick", Kind: search.VocabKindArtist, MatchScore: 0.8}}, nil
			}
			return []search.SpellingHintsEntry{{Term: "Lamar", TermNorm: "lamar", Kind: search.VocabKindArtist, MatchScore: 1}}, nil
		},
	}
	s := searchtest.NewCorrection(t, store)
	got := s.CorrectAggressive(context.Background(), "kendrik lamar")
	if got == nil || got.Corrected != "kendrick lamar" {
		t.Fatalf("aggressive = %+v, want token-corrected \"kendrick lamar\"", got)
	}
	if got.Confidence != 0.8 {
		t.Errorf("confidence = %v, want the min corrected-token score 0.8", got.Confidence)
	}
}
