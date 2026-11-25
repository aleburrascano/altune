package core_test

import (
	"altune/overseer/internal/core"
	"testing"
	"time"
)

func TestDiscardSeriesKeepsNothingItIsGiven(t *testing.T) {
	var s core.Series = core.DiscardSeries{}
	now := time.Now()
	s.Record("cost", "spend", core.Point{At: now, Value: 1})

	got, err := s.Query("cost", "spend", now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("query a discarding series: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a discarding series returned %d points, want none", len(got))
	}
}
