package metrics

import (
	"expvar"
	"strconv"
	"testing"
)

func varValue(t *testing.T, name string) int64 {
	t.Helper()
	v := expvar.Get(name)
	if v == nil {
		t.Fatalf("expvar %q was never published", name)
	}
	n, err := strconv.ParseInt(v.String(), 10, 64)
	if err != nil {
		t.Fatalf("expvar %q = %q, not an integer: %v", name, v.String(), err)
	}
	return n
}

func TestExpvarAudioStoreMetrics_PublishesAndIncrements(t *testing.T) {
	m := NewExpvarAudioStoreMetrics()

	cases := []struct {
		name    string
		varName string
		inc     func()
	}{
		{"presign failures", PresignFailuresVar, m.PresignFailed},
		{"orphaned deletes", OrphanedDeletesVar, m.OrphanedDelete},
		{"stream recoveries", StreamRecoveriesVar, m.StreamRecoveryTriggered},
		{"orphaned audio reconcile failures", OrphanedAudioReconcileFailuresVar, m.OrphanedAudioReconcileFailed},
		{"db call timeouts", DBCallTimeoutsVar, NewExpvarDBCallMetrics().DBCallTimedOut},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := varValue(t, tc.varName)
			tc.inc()
			if after := varValue(t, tc.varName); after != before+1 {
				t.Errorf("%s = %d, want %d after one increment", tc.varName, after, before+1)
			}
		})
	}
}

// TestReadSnapshot_CarriesOrphanedAudioReconcileFailures pins #2198: the sweep
// failure counter must reach the operator snapshot (GET /observe/metrics/live),
// not only the raw expvar registry that endpoint deliberately does not expose.
func TestReadSnapshot_CarriesOrphanedAudioReconcileFailures(t *testing.T) {
	before := ReadSnapshot().OrphanedAudioReconcileFailures
	NewExpvarAudioStoreMetrics().OrphanedAudioReconcileFailed()
	if after := ReadSnapshot().OrphanedAudioReconcileFailures; after != before+1 {
		t.Errorf("snapshot orphaned_audio_reconcile_failures_total = %d, want %d", after, before+1)
	}
}
