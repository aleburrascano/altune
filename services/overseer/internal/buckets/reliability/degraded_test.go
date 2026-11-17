package reliability_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"testing"
)

func TestPollerReportsDegradedNotDown(t *testing.T) {
	b, probe := probedBucket(t, &fakeReader{})

	probe.answer(degraded())

	snap := b.Snapshot()
	if snap.State == core.StateSourceDown {
		t.Errorf("state = %q, want reachable: go-api answered, just degraded", snap.State)
	}
	if snap.Reason != goapi.ReasonDegraded {
		t.Errorf("Reason = %q, want %q", snap.Reason, goapi.ReasonDegraded)
	}
	if got := snapData(t, snap).Reachability; got != goapi.ReasonDegraded {
		t.Errorf("reachability = %q, want %q", got, goapi.ReasonDegraded)
	}
}

func TestSnapshotReasonIsDegradedOn503WithBody(t *testing.T) {
	reader := &fakeReader{}
	reader.set(healthyHealth(), nil)

	b, probe := probedBucket(t, reader)
	if err := collectStore(t, b); err != nil {
		t.Fatalf("Collect = %v, want nil", err)
	}
	probe.answer(degraded())

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
