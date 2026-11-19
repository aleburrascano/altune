package eval_test

import (
	"altune/go-api/internal/discovery"
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"
)

type sleepingSearcher map[string]time.Duration

func (s sleepingSearcher) Search(_ context.Context, query string) ([]discovery.SearchResult, error) {
	time.Sleep(s[query])
	return nil, nil
}

func TestRunHealthEval_LatencyPercentilesAreNearestRank(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var entities []discovery.LibraryEntity
		searcher := sleepingSearcher{}
		for i := 1; i <= 10; i++ {
			artist := fmt.Sprintf("artist%d", i)
			entities = append(entities, discovery.LibraryEntity{Title: "song", Artist: artist})
			searcher[artist+" song"] = time.Duration(i*10) * time.Millisecond
		}

		r := discovery.RunHealthEval(context.Background(), entities, searcher, 1, nil)

		if r.LatencyP50Ms != 60 || r.LatencyP95Ms != 100 || r.LatencyMaxMs != 100 {
			t.Errorf("p50/p95/max = %d/%d/%d ms, want 60/100/100", r.LatencyP50Ms, r.LatencyP95Ms, r.LatencyMaxMs)
		}
	})
}

func TestRunHealthEval_NoSearchesReportZeroLatency(t *testing.T) {
	r := discovery.RunHealthEval(context.Background(), nil, sleepingSearcher{}, 1, nil)

	if r.LatencyP50Ms != 0 || r.LatencyMaxMs != 0 {
		t.Errorf("p50/max = %d/%d ms, want 0/0", r.LatencyP50Ms, r.LatencyMaxMs)
	}
}

func TestRunHealthEval_fill(t *testing.T) {
	entities := []discovery.LibraryEntity{{Title: "Humble", Artist: "Kendrick"}}
	withArt := track("Humble", "Kendrick", discovery.ProviderDeezer, nil)
	withArt.ImageURL = "https://art/1.jpg"
	noArt := track("Other", "Kendrick", discovery.ProviderITunes, nil)

	fake := &fakeSearcher{byQuery: map[string][]discovery.SearchResult{
		"Kendrick Humble": {withArt, noArt},
	}}
	r := discovery.RunHealthEval(context.Background(), entities, fake, 1, nil)

	if r.Results != 2 {
		t.Fatalf("expected 2 results seen, got %d", r.Results)
	}
	if r.WithArtwork != 1 || r.FillRate != 0.5 {
		t.Errorf("fill: with_artwork=%d rate=%v, want 1 / 0.5", r.WithArtwork, r.FillRate)
	}
	if len(r.HealthMetrics()) != 3 {
		t.Errorf("expected 3 health metrics, got %d", len(r.HealthMetrics()))
	}
}
