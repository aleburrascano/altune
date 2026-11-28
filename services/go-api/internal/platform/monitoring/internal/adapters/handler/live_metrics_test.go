package handler_test

import (
	"altune/go-api/internal/platform/monitoring/internal/adapters/handler"
	"altune/go-api/internal/platform/monitoring/internal/adapters/notifier"
	"altune/go-api/internal/platform/monitoring/internal/domain"
	"altune/go-api/internal/platform/monitoring/internal/service"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/events/eventstest"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type fixedSnapshot struct{ value any }

func (f fixedSnapshot) Snapshot() any { return f.value }

type fixedLatency struct{ latency domain.RequestLatency }

func (f fixedLatency) RequestLatency() domain.RequestLatency { return f.latency }

type fixedLeader struct{ counters domain.LeaderCounters }

func (f fixedLeader) LeaderCounters() domain.LeaderCounters { return f.counters }

type liveSources struct {
	handler.LiveSources
	bus *events.InProcessBus
}

func newLiveSources(t *testing.T) liveSources {
	t.Helper()
	bus := events.NewInProcessBus(context.Background(), time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	tap := service.MustNewEventTap(bus, time.Now)
	alertMonitor, err := service.NewAlertMonitor(notifier.NopNotifier{}, time.Second, time.Now)
	if err != nil {
		t.Fatalf("NewAlertMonitor: %v", err)
	}
	empty := fixedSnapshot{value: struct{}{}}
	return liveSources{
		LiveSources: handler.LiveSources{
			Auth:         empty,
			Catalog:      empty,
			Playback:     empty,
			Providers:    empty,
			Latency:      fixedLatency{},
			Leader:       fixedLeader{},
			PoolStats:    func() database.PoolStats { return database.PoolStats{} },
			EventBus:     bus,
			EventTap:     tap,
			EventFeed:    service.NewEventFeed(),
			AlertMonitor: alertMonitor,
			Background:   &background.Tracker{},
		},
		bus: bus,
	}
}

func servedLive(t *testing.T, s handler.LiveSources) []byte {
	t.Helper()
	r := chi.NewRouter()
	handler.NewObserveHandler(clock.Now, clock.NewTicker, handler.Deps{LiveMetrics: handler.NewLiveMetrics(s)}).Register(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics/live", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics/live = %d, want 200; body %s", rec.Code, rec.Body)
	}
	return rec.Body.Bytes()
}

func decodeLive(t *testing.T, raw []byte, into any) {
	t.Helper()
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

func TestLive_CarriesEverySourceUnderItsOwnKey(t *testing.T) {
	s := newLiveSources(t)
	s.Leader = fixedLeader{counters: domain.LeaderCounters{Failures: 7}}

	var out map[string]json.RawMessage
	decodeLive(t, servedLive(t, s.LiveSources), &out)

	for _, key := range []string{"auth", "catalog", "playback", "providers", "latency", "db_pool", "redis_pool", "leader", "event_bus", "alert_monitor", "event_tap"} {
		if _, ok := out[key]; !ok {
			t.Errorf("live metrics missing key %q", key)
		}
	}
	var l struct {
		Failures int64 `json:"acquire_failures_total"`
	}
	decodeLive(t, out["leader"], &l)
	if l.Failures != 7 {
		t.Errorf("leader failures = %d, want 7", l.Failures)
	}
	var monitor map[string]json.RawMessage
	decodeLive(t, out["alert_monitor"], &monitor)
	for _, key := range []string{"last_pass", "consecutive_failures", "contained_panics", "nop_notifier"} {
		if _, ok := monitor[key]; !ok {
			t.Errorf("alert_monitor missing key %q", key)
		}
	}
	if string(monitor["nop_notifier"]) != "true" {
		t.Errorf("nop_notifier = %s, want true", monitor["nop_notifier"])
	}
}

func TestLive_LeaderAndAlertMonitorKeepTheirWireKeys(t *testing.T) {
	src := newLiveSources(t)
	src.Leader = fixedLeader{counters: domain.LeaderCounters{Attempts: 4, Wins: 3, Contended: 1, Failures: 7, TermEnds: 2, HoldsLost: 5}}

	raw := servedLive(t, src.LiveSources)

	var got struct {
		Leader       map[string]int64           `json:"leader"`
		AlertMonitor map[string]json.RawMessage `json:"alert_monitor"`
	}
	decodeLive(t, raw, &got)
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
	src := newLiveSources(t)
	src.Latency = fixedLatency{latency: domain.RequestLatency{Routes: map[string]domain.RouteLatency{
		"/v1/probe/{id}": {
			Count: 1, SumMs: 5,
			Buckets: []domain.LatencyBucket{{LeMs: "10", Count: 1}, {LeMs: "+Inf", Count: 1}},
			Status:  domain.StatusClasses{Count5xx: 1},
		},
	}}}

	raw := servedLive(t, src.LiveSources)

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
	decodeLive(t, raw, &got)
	route, ok := got.Latency.Routes["/v1/probe/{id}"]
	if !ok || route.Count != 1 || len(route.Buckets) == 0 || route.Status["5xx"] != 1 {
		t.Fatalf("latency route = %+v (found %v), want one 5xx request with buckets: %s", route, ok, raw)
	}
	if last := route.Buckets[len(route.Buckets)-1]; last.LeMs != "+Inf" {
		t.Errorf("final bucket label = %q, want +Inf", last.LeMs)
	}
}

func TestLive_ReportsDroppedBackgroundTasks(t *testing.T) {
	s := newLiveSources(t)
	bg, err := background.NewTracker(background.TrackerLimits{MaxInFlight: 1})
	if err != nil {
		t.Fatal(err)
	}
	s.Background = bg
	release := make(chan struct{})
	started := make(chan struct{})
	t.Cleanup(func() { close(release); bg.Wait() })
	if !bg.LaunchWithin(context.Background(), "hold", time.Minute, func(context.Context) { close(started); <-release }) {
		t.Fatal("precondition: the first task was refused")
	}
	<-started
	for range 3 {
		if bg.LaunchWithin(context.Background(), "over the cap", time.Minute, func(context.Context) {}) {
			t.Fatal("precondition: a task over the cap was admitted")
		}
	}

	var out struct {
		Dropped uint64 `json:"background_tasks_dropped"`
	}
	decodeLive(t, servedLive(t, s.LiveSources), &out)

	if out.Dropped != 3 {
		t.Errorf("background_tasks_dropped = %d, want 3", out.Dropped)
	}
}

func TestLive_ReportsEventBusDrops(t *testing.T) {
	s := newLiveSources(t)
	user := ids.NewUserId(uuid.New())
	_, unsubscribe := s.bus.Subscribe(user)
	t.Cleanup(unsubscribe)
	for i := 0; i < 10000 && s.bus.Dropped() == 0; i++ {
		s.bus.Publish(context.Background(), user, eventstest.Named("probe"))
	}
	if s.bus.Dropped() == 0 {
		t.Fatal("precondition: an unread subscriber never overflowed, so the bus dropped nothing")
	}

	var out struct {
		EventBus struct {
			Dropped uint64 `json:"dropped_total"`
		} `json:"event_bus"`
	}
	decodeLive(t, servedLive(t, s.LiveSources), &out)

	if out.EventBus.Dropped != s.bus.Dropped() {
		t.Errorf("event_bus.dropped_total = %d, want the bus's %d", out.EventBus.Dropped, s.bus.Dropped())
	}
}

func TestLive_ReportsEventTapAndFeedDrops(t *testing.T) {
	s := newLiveSources(t)
	ctx, stop := context.WithCancel(context.Background())
	s.EventFeed.Start(ctx, s.EventTap)
	t.Cleanup(func() {
		stop()
		s.EventFeed.Shutdown(context.Background())
	})
	_, cancel, err := s.EventFeed.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)
	user := ids.NewUserId(uuid.New())
	deadline := time.Now().Add(5 * time.Second)
	for s.EventFeed.Dropped() == 0 && time.Now().Before(deadline) {
		s.EventTap.Publish(context.Background(), user, eventstest.Named("flood"))
	}
	if s.EventFeed.Dropped() == 0 {
		t.Fatal("precondition: an unread subscriber never overflowed, so the feed dropped nothing")
	}

	var out struct {
		EventTap struct {
			TapDropped  uint64 `json:"tap_dropped_total"`
			FeedDropped uint64 `json:"feed_dropped_total"`
		} `json:"event_tap"`
	}
	decodeLive(t, servedLive(t, s.LiveSources), &out)

	if out.EventTap.FeedDropped == 0 {
		t.Errorf("event_tap.feed_dropped_total = 0, want the feed's drops")
	}
	if out.EventTap.TapDropped != s.EventTap.Dropped() {
		t.Errorf("event_tap.tap_dropped_total = %d, want the tap's %d", out.EventTap.TapDropped, s.EventTap.Dropped())
	}
}
