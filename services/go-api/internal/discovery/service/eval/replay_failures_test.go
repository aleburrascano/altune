package eval

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"testing"
)

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
