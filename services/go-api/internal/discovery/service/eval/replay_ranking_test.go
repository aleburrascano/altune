package eval

import (
	"context"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

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
