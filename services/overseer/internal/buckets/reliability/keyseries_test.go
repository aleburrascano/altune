package reliability

import "testing"

func TestKeySeriesIsLatencyMS(t *testing.T) {
	b := newBucket(&fakeReader{}, &fakeChecker{}, defaultPollInterval)
	if got := b.KeySeries(); got != seriesLatencyMS {
		t.Errorf("KeySeries() = %q, want %q", got, seriesLatencyMS)
	}
}
