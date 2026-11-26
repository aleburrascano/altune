package metrics_test

import (
	"altune/go-api/internal/library/internal/adapters/metrics"
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
	m := metrics.NewExpvarAudioStoreMetrics()

	cases := []struct {
		name    string
		varName string
		inc     func()
	}{
		{"presign failures", metrics.PresignFailuresVar, m.PresignFailed},
		{"orphaned deletes", metrics.OrphanedDeletesVar, m.OrphanedDelete},
		{"stream recoveries", metrics.StreamRecoveriesVar, m.StreamRecoveryTriggered},
		{"orphaned audio reconcile failures", metrics.OrphanedAudioReconcileFailuresVar, m.OrphanedAudioReconcileFailed},
		{"db call timeouts", metrics.DBCallTimeoutsVar, metrics.NewExpvarDBCallMetrics().DBCallTimedOut},
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

func TestReadSnapshot_CarriesOrphanedAudioReconcileFailures(t *testing.T) {
	before := metrics.ReadSnapshot().OrphanedAudioReconcileFailures
	metrics.NewExpvarAudioStoreMetrics().OrphanedAudioReconcileFailed()
	if after := metrics.ReadSnapshot().OrphanedAudioReconcileFailures; after != before+1 {
		t.Errorf("snapshot orphaned_audio_reconcile_failures_total = %d, want %d", after, before+1)
	}
}
