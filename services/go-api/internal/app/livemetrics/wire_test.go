package livemetrics_test

import (
	"altune/go-api/internal/app/livemetrics"
	"altune/go-api/internal/platform/leader"
	"encoding/json"
	"testing"
	"time"
)

func TestLive_LeaderAndAlertMonitorKeepTheirWireKeys(t *testing.T) {
	src := newSources(t)
	src.Leader = leader.Counters{Attempts: 4, Wins: 3, Contended: 1, Failures: 7, TermEnds: 2, HoldsLost: 5}

	raw, err := json.Marshal(livemetrics.Live(src.Sources))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got struct {
		Leader       map[string]int64           `json:"leader"`
		AlertMonitor map[string]json.RawMessage `json:"alert_monitor"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	wantLeader := map[string]int64{
		"acquire_attempts_total": 4, "acquire_wins_total": 3, "acquire_contended_total": 1,
		"acquire_failures_total": 7, "term_ends_total": 2, "holds_lost_total": 5,
	}
	for key, want := range wantLeader {
		if got.Leader[key] != want {
			t.Errorf("leader[%q] = %d, want %d", key, got.Leader[key], want)
		}
	}
	for _, key := range []string{"last_pass", "last_notify_ok", "consecutive_failures", "contained_panics", "nop_notifier", "last_notify_ok_at", "last_notify_error_at"} {
		if _, ok := got.AlertMonitor[key]; !ok {
			t.Errorf("alert_monitor lacks %q: %s", key, raw)
		}
	}
}

func TestLive_LatencyKeepsItsWireKeys(t *testing.T) {
	src := newSources(t)
	src.Latency.Observe("/v1/probe/{id}", 5*time.Millisecond, 503)

	raw, err := json.Marshal(livemetrics.Live(src.Sources))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got struct {
		Latency struct {
			Routes map[string]struct {
				Count   uint64 `json:"count"`
				SumMs   uint64 `json:"sum_ms"`
				Buckets []struct {
					LeMs  string `json:"le_ms"`
					Count uint64 `json:"count"`
				} `json:"buckets"`
				Status map[string]uint64 `json:"status"`
			} `json:"routes"`
		} `json:"latency"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	route, ok := got.Latency.Routes["/v1/probe/{id}"]
	if !ok || route.Count != 1 || len(route.Buckets) == 0 || route.Status["5xx"] != 1 {
		t.Fatalf("latency route = %+v (found %v), want one 5xx request with buckets: %s", route, ok, raw)
	}
	if last := route.Buckets[len(route.Buckets)-1]; last.LeMs != "+Inf" {
		t.Errorf("final bucket label = %q, want +Inf", last.LeMs)
	}
}
