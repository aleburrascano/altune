package eval

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"testing"
)

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
