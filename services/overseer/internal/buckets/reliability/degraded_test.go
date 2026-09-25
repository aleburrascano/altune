package reliability

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"testing"
)

func TestPollerReportsDegradedNotDown(t *testing.T) {
	checker := &fakeChecker{}
	checker.set(goapi.Health{Status: "degraded"}, nil)
	p := newReachPoller(checker, defaultPollInterval)

	p.pollOnce(context.Background())

	if got := p.currentStatus(); got != goapi.StatusUp {
		t.Errorf("status = %v, want up (reachable, just degraded)", got)
	}
	if got := p.degradedReason(); got != goapi.ReasonDegraded {
		t.Errorf("degradedReason = %q, want %q", got, goapi.ReasonDegraded)
	}
}

func TestSnapshotReasonIsDegradedOn503WithBody(t *testing.T) {
	reader := &fakeReader{}
	checker := &fakeChecker{}
	reader.set(healthyHealth(), nil)
	checker.set(goapi.Health{Status: "degraded"}, nil)

	b := newBucket(reader, checker, defaultPollInterval)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect = %v, want nil", err)
	}
	b.poller.pollOnce(context.Background())

	snap := b.Snapshot()
	if snap.Reason != goapi.ReasonDegraded {
		t.Errorf("Reason = %q, want %q", snap.Reason, goapi.ReasonDegraded)
	}
	if snap.State != core.StateLive {
		t.Errorf("state = %q, want live — go-api is reachable, just degraded", snap.State)
	}
	if snap.Severity != core.SeverityWarn {
		t.Errorf("severity = %q, want warn", snap.Severity)
	}
}
