package reliability_test

import (
	"altune/overseer/internal/core"
	"testing"
)

func TestDegradedProbeCountsAsReachableForUptime(t *testing.T) {
	b, probe := probedBucket(t, &fakeReader{})

	probe.answer(degraded())
	probe.answer(up())

	snap := b.Snapshot()

	if snap.Headline != "uptime 100.0%" {
		t.Errorf("headline = %q, want uptime 100.0%% with no flap", snap.Headline)
	}
	if snap.Severity != core.SeverityOK {
		t.Errorf("severity = %q, want ok", snap.Severity)
	}
}

func TestDegradedWindowReportsFullUptime(t *testing.T) {
	b, probe := probedBucket(t, &fakeReader{})

	probe.answer(degraded())
	probe.answer(degraded())

	if got := b.Snapshot().Headline; got != "go-api degraded · uptime 100.0%" {
		t.Errorf("headline = %q, want go-api degraded at uptime 100.0%%", got)
	}
}
