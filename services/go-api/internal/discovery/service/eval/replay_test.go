package eval

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"math"
	"testing"
)

func TestReplayCorpus_ScoresPositivesAndNegativeLeak(t *testing.T) {
	corpus := BehavioralCorpus{Entries: []BehavioralCorpusEntry{
		{Query: "adele hello", ResultSignature: "good", Polarity: 1},
		{Query: "drake", ResultSignature: "missing", Polarity: 1},
		{Query: "wrong", ResultSignature: "bad", Polarity: -1},
	}}
	ranking := CandidateRanking{
		"adele hello": {"good", "x", "y"},
		"drake":       {"a", "b"},
		"wrong":       {"z", "bad"},
	}

	score := ReplayCorpus(corpus, ranking, 3)

	if score.Positives != 2 || score.Found != 1 {
		t.Errorf("positives=%d found=%d, want 2/1", score.Positives, score.Found)
	}
	if math.Abs(score.MRR-0.5) > 1e-9 {
		t.Errorf("MRR = %v, want 0.5", score.MRR)
	}
	if score.Negatives != 1 || score.NegativeLeakK != 1 {
		t.Errorf("negatives=%d leak=%d, want 1/1", score.Negatives, score.NegativeLeakK)
	}
}

func TestReplayCorpus_NegativeBelowTopKDoesNotLeak(t *testing.T) {
	corpus := BehavioralCorpus{Entries: []BehavioralCorpusEntry{
		{Query: "q", ResultSignature: "bad", Polarity: -1},
	}}
	ranking := CandidateRanking{"q": {"a", "b", "c", "bad"}}
	score := ReplayCorpus(corpus, ranking, 3)
	if score.NegativeLeakK != 0 {
		t.Errorf("negative at rank 3 must not leak into top-3, got %d", score.NegativeLeakK)
	}
}

type failingReplaySearcher struct{}

func (failingReplaySearcher) Search(context.Context, string) ([]domain.SearchResult, error) {
	return nil, errors.New("provider down")
}

func TestBuildRankingCountingFailures_CountsFailedQueries(t *testing.T) {
	corpus := BehavioralCorpus{Entries: []BehavioralCorpusEntry{
		{Query: "a", ResultSignature: "x", Polarity: 1},
		{Query: "a", ResultSignature: "y", Polarity: -1},
		{Query: "b", ResultSignature: "z", Polarity: 1},
	}}
	ranking, failed := BuildRankingCountingFailures(context.Background(), corpus, failingReplaySearcher{})
	if failed != 2 {
		t.Fatalf("failed = %d, want 2 distinct queries", failed)
	}
	if len(ranking) != 0 {
		t.Fatalf("ranking = %v, want empty", ranking)
	}
}

type fakeReplaySearcher struct {
	byQuery map[string][]domain.SearchResult
	calls   map[string]int
}

func (f *fakeReplaySearcher) Search(_ context.Context, query string) ([]domain.SearchResult, error) {
	f.calls[query]++
	return f.byQuery[query], nil
}

func TestBuildRanking_OrdersBySearchResultAndDedupesQueries(t *testing.T) {
	searcher := &fakeReplaySearcher{
		calls: map[string]int{},
		byQuery: map[string][]domain.SearchResult{
			"adele hello": {
				{Kind: domain.ResultKindTrack, Title: "Hello", Subtitle: "Adele"},
				{Kind: domain.ResultKindTrack, Title: "Hello Again", Subtitle: "Someone Else"},
			},
		},
	}
	corpus := BehavioralCorpus{Entries: []BehavioralCorpusEntry{
		{Query: "adele hello", ResultSignature: domain.ResultSignature(domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Hello", Subtitle: "Adele"}), Polarity: 1},
		{Query: "adele hello", ResultSignature: "irrelevant-second-entry-same-query", Polarity: -1},
	}}

	ranking := BuildRanking(context.Background(), corpus, searcher)

	if searcher.calls["adele hello"] != 1 {
		t.Fatalf("expected the query to be searched once despite two corpus entries, got %d calls", searcher.calls["adele hello"])
	}
	order, ok := ranking["adele hello"]
	if !ok || len(order) != 2 {
		t.Fatalf("expected a 2-entry ranking for %q, got %v", "adele hello", order)
	}
	if order[0] != domain.ResultSignature(domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Hello", Subtitle: "Adele"}) {
		t.Errorf("ranking[0] = %q, want the top search result's signature", order[0])
	}
}

func TestReplayCorpus_EndToEndAgainstBuiltRanking(t *testing.T) {
	searcher := &fakeReplaySearcher{
		calls: map[string]int{},
		byQuery: map[string][]domain.SearchResult{
			"adele hello": {{Kind: domain.ResultKindTrack, Title: "Hello", Subtitle: "Adele"}},
		},
	}
	goodSig := domain.ResultSignature(domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Hello", Subtitle: "Adele"})
	corpus := BehavioralCorpus{Entries: []BehavioralCorpusEntry{
		{Query: "adele hello", ResultSignature: goodSig, Polarity: 1},
	}}

	ranking := BuildRanking(context.Background(), corpus, searcher)
	score := ReplayCorpus(corpus, ranking, 3)

	if score.Positives != 1 || score.Found != 1 {
		t.Fatalf("positives=%d found=%d, want 1/1", score.Positives, score.Found)
	}
	if score.MRR != 1.0 {
		t.Errorf("MRR = %v, want 1.0 (result at rank 1)", score.MRR)
	}
}

type stampedSigSearcher struct {
	results []domain.SearchResult
}

func (f stampedSigSearcher) Search(context.Context, string) ([]domain.SearchResult, error) {
	return f.results, nil
}

func TestBuildRanking_PrefersStampedSignatureOverDerived(t *testing.T) {
	result := domain.SearchResult{
		Kind:      domain.ResultKindTrack,
		Title:     "Hello",
		Subtitle:  "",
		Signature: "stamped-adele-hello",
	}
	searcher := stampedSigSearcher{results: []domain.SearchResult{result}}
	corpus := BehavioralCorpus{Entries: []BehavioralCorpusEntry{
		{Query: "adele hello", ResultSignature: "stamped-adele-hello", Polarity: 1},
	}}

	ranking := BuildRanking(context.Background(), corpus, searcher)
	score := ReplayCorpus(corpus, ranking, 3)

	if derived := domain.ResultSignature(result); derived == result.Signature {
		t.Fatalf("test setup invalid: derived signature %q must differ from stamped %q", derived, result.Signature)
	}
	if score.Positives != 1 || score.Found != 1 {
		t.Fatalf("positives=%d found=%d, want 1/1 when matching against the stamped signature", score.Positives, score.Found)
	}
}
