package reliability_test

import "testing"

func TestKeySeriesIsLatencyMS(t *testing.T) {
	if got := newBucket(&fakeReader{}).KeySeries(); got != "latency_ms" {
		t.Errorf("KeySeries() = %q, want latency_ms", got)
	}
}
