package reliability

import (
	"altune/overseer/internal/goapi"
	"context"
	"testing"
)

// TestAnsweredProbeWritesUnderTheDeclaredKeySeries proves offline, with zero
// network, that a healthy reachability probe records under exactly the name
// KeySeries() declares — not just that the constant equals itself
// (keyseries_test.go), but that the write site (poller.go's recordProbe)
// actually uses that name.
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

// TestUnansweredProbeWritesNothingUnderTheDeclaredKeySeries proves the flip
// side: a probe that never got an answer must record nothing under
// KeySeries() — an unreachable go-api has no latency to report.
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
