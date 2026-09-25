package app

import (
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/leader"
	"encoding/json"
	"testing"
)

type counterElection struct {
	fakeElection
	counters leader.Counters
}

func (c *counterElection) Counters() leader.Counters { return c.counters }

func TestLiveMetrics_CarriesSaturationKeys(t *testing.T) {
	a := &App{
		election: &counterElection{counters: leader.Counters{Failures: 7}},
		eventBus: events.NewInProcessBus(),
	}
	raw, err := json.Marshal(a.liveMetrics())
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"db_pool", "redis_pool", "leader", "event_bus", "latency"} {
		if _, ok := out[key]; !ok {
			t.Errorf("live metrics missing key %q", key)
		}
	}
	var l leader.Counters
	if err := json.Unmarshal(out["leader"], &l); err != nil {
		t.Fatal(err)
	}
	if l.Failures != 7 {
		t.Errorf("leader failures = %d, want 7", l.Failures)
	}
}
