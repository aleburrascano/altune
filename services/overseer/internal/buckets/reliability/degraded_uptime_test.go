package reliability

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"testing"
)

func TestDegradedProbeCountsAsReachableForUptime(t *testing.T) {
	checker := &fakeChecker{}
	b := newBucket(&fakeReader{}, checker, defaultPollInterval)

	checker.set(goapi.Health{Status: "degraded"}, nil)
	b.poller.pollOnce(context.Background())
	checker.set(goapi.Health{Status: "ok"}, nil)
	b.poller.pollOnce(context.Background())

	snap := b.Snapshot()

	if snap.Headline != "uptime 100.0%" {
		t.Errorf("headline = %q, want uptime 100.0%% with no flap", snap.Headline)
	}
	if snap.Severity != core.SeverityOK {
		t.Errorf("severity = %q, want ok", snap.Severity)
	}
}

func TestDegradedWindowReportsFullUptime(t *testing.T) {
	checker := &fakeChecker{}
	checker.set(goapi.Health{Status: "degraded"}, nil)
	b := newBucket(&fakeReader{}, checker, defaultPollInterval)

	b.poller.pollOnce(context.Background())
	b.poller.pollOnce(context.Background())

	if got := b.Snapshot().Headline; got != "go-api degraded · uptime 100.0%" {
		t.Errorf("headline = %q, want go-api degraded at uptime 100.0%%", got)
	}
}
