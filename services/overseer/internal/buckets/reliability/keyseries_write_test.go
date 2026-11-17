package reliability_test

import (
	"altune/overseer/internal/goapi"
	"testing"
)

func TestAnsweredProbeWritesUnderTheDeclaredKeySeries(t *testing.T) {
	b, probe, series := bucketWithSeries(t, nil)

	probe.answer(up())

	name := b.KeySeries()
	if got := series.named(name); len(got) != 1 {
		t.Fatalf("series recorded under KeySeries() (%q) = %+v, want exactly one point", name, got)
	}
}

func TestUnansweredProbeWritesNothingUnderTheDeclaredKeySeries(t *testing.T) {
	b, probe, series := bucketWithSeries(t, nil)

	probe.answer(goapi.Health{}, srcDown("GET /health"))

	name := b.KeySeries()
	if got := series.named(name); len(got) != 0 {
		t.Fatalf("series recorded under KeySeries() (%q) = %+v, want none: probe was unanswered", name, got)
	}
}
