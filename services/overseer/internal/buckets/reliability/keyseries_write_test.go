package reliability

import (
	"altune/overseer/internal/goapi"
	"context"
	"testing"
)

func TestAnsweredProbeWritesUnderTheDeclaredKeySeries(t *testing.T) {
	checker := &fakeChecker{}
	checker.set(goapi.Health{Status: "ok"}, nil)
	b, series := bucketWithSeries(checker)

	b.poller.pollOnce(context.Background())

	name := b.KeySeries()
	if got := series.named(name); len(got) != 1 {
		t.Fatalf("series recorded under KeySeries() (%q) = %+v, want exactly one point", name, got)
	}
}

func TestUnansweredProbeWritesNothingUnderTheDeclaredKeySeries(t *testing.T) {
	checker := &fakeChecker{}
	checker.set(goapi.Health{}, srcDown("GET /health"))
	b, series := bucketWithSeries(checker)

	b.poller.pollOnce(context.Background())

	name := b.KeySeries()
	if got := series.named(name); len(got) != 0 {
		t.Fatalf("series recorded under KeySeries() (%q) = %+v, want none: probe was unanswered", name, got)
	}
}
