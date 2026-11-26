package livemetrics_test

import (
	"altune/go-api/internal/app/livemetrics"
	"altune/go-api/internal/library"
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/platform/leader"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/requestmetrics"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/events/eventstest"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type sources struct {
	livemetrics.Sources
	bus *events.InProcessBus
}

func newSources(t *testing.T) sources {
	t.Helper()
	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	tap := monitoring.NewEventTap(bus, time.Now)
	alertMonitor, err := monitoring.NewAlertMonitor(monitoring.NopNotifier{}, time.Second, time.Now)
	if err != nil {
		t.Fatalf("NewAlertMonitor: %v", err)
	}
	return sources{
		Sources: livemetrics.Sources{
			Latency:      requestmetrics.NewRegistry(),
			EventBus:     bus,
			EventTap:     tap,
			EventFeed:    monitoring.NewEventFeed(),
			AlertMonitor: alertMonitor,
			Background:   &background.Tracker{},
		},
		bus: bus,
	}
}

func served(t *testing.T, s livemetrics.Sources) []byte {
	t.Helper()
	r := chi.NewRouter()
	monitoring.NewObserveHandler(monitoring.Deps{LiveMetrics: func() monitoring.LiveMetrics { return livemetrics.Live(s) }}).Register(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics/live", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics/live = %d, want 200; body %s", rec.Code, rec.Body)
	}
	return rec.Body.Bytes()
}

func decode(t *testing.T, raw []byte, into any) {
	t.Helper()
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

func TestLive_CarriesEverySourceUnderItsOwnKey(t *testing.T) {
	s := newSources(t)
	s.Leader = leader.Counters{Failures: 7}

	var out map[string]json.RawMessage
	decode(t, served(t, s.Sources), &out)

	for _, key := range []string{"auth", "catalog", "playback", "providers", "latency", "db_pool", "redis_pool", "leader", "event_bus", "alert_monitor", "event_tap"} {
		if _, ok := out[key]; !ok {
			t.Errorf("live metrics missing key %q", key)
		}
	}
	var l leader.Counters
	decode(t, out["leader"], &l)
	if l.Failures != 7 {
		t.Errorf("leader failures = %d, want 7", l.Failures)
	}
	var monitor map[string]json.RawMessage
	decode(t, out["alert_monitor"], &monitor)
	for _, key := range []string{"last_pass", "consecutive_failures", "contained_panics", "nop_notifier"} {
		if _, ok := monitor[key]; !ok {
			t.Errorf("alert_monitor missing key %q", key)
		}
	}
	if string(monitor["nop_notifier"]) != "true" {
		t.Errorf("nop_notifier = %s, want true", monitor["nop_notifier"])
	}
}

func TestLive_ReportsDroppedBackgroundTasks(t *testing.T) {
	s := newSources(t)
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
	decode(t, served(t, s.Sources), &out)

	if out.Dropped != 3 {
		t.Errorf("background_tasks_dropped = %d, want 3", out.Dropped)
	}
}

func TestLive_ReportsEventBusDrops(t *testing.T) {
	s := newSources(t)
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
	decode(t, served(t, s.Sources), &out)

	if out.EventBus.Dropped != s.bus.Dropped() {
		t.Errorf("event_bus.dropped_total = %d, want the bus's %d", out.EventBus.Dropped, s.bus.Dropped())
	}
}

func TestLive_ReportsEventTapAndFeedDrops(t *testing.T) {
	s := newSources(t)
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
	decode(t, served(t, s.Sources), &out)

	if out.EventTap.FeedDropped == 0 {
		t.Errorf("event_tap.feed_dropped_total = 0, want the feed's drops")
	}
	if out.EventTap.TapDropped != s.EventTap.Dropped() {
		t.Errorf("event_tap.tap_dropped_total = %d, want the tap's %d", out.EventTap.TapDropped, s.EventTap.Dropped())
	}
}

type counterWire struct {
	Auth struct {
		TokenRejections     int64            `json:"token_rejections_total"`
		ByReason            map[string]int64 `json:"token_rejections_by_reason_total"`
		VerifierUnavailable int64            `json:"verifier_unavailable_total"`
		JWKSFetchFailures   int64            `json:"jwks_fetch_failures_total"`
	} `json:"auth"`
	Catalog struct {
		PresignFailures int64 `json:"presign_failures_total"`
	} `json:"catalog"`
	Playback struct {
		EnrichmentFailures       int64 `json:"now_playing_enrichment_failures_total"`
		CorruptStoredState       int64 `json:"corrupt_stored_state_total"`
		QueueStateOpTimeouts     int64 `json:"queue_state_op_timeouts_total"`
		NowPlayingLookupTimeouts int64 `json:"now_playing_lookup_timeouts_total"`
	} `json:"playback"`
}

func TestLive_CarriesAuthCatalogAndPlaybackFailureCounters(t *testing.T) {
	s := newSources(t).Sources
	var before counterWire
	decode(t, served(t, s), &before)

	auth.NewExpvarAuthMetrics().TokenRejected("signature_invalid")
	auth.NewExpvarAuthMetrics().VerifierUnavailable()
	auth.NewExpvarAuthMetrics().JWKSFetchFailed()
	library.NewExpvarAudioStoreMetrics().PresignFailed()
	pb := playback.NewExpvarPlaybackMetrics()
	pb.EnrichmentFailed()
	pb.CorruptStoredState()
	pb.QueueStateOpTimedOut()
	pb.NowPlayingLookupTimedOut()

	var got counterWire
	decode(t, served(t, s), &got)
	for _, c := range []struct {
		key       string
		got, want int64
	}{
		{"auth.token_rejections_total", got.Auth.TokenRejections, before.Auth.TokenRejections + 1},
		{"auth.token_rejections_by_reason_total[signature_invalid]", got.Auth.ByReason["signature_invalid"], before.Auth.ByReason["signature_invalid"] + 1},
		{"auth.verifier_unavailable_total", got.Auth.VerifierUnavailable, before.Auth.VerifierUnavailable + 1},
		{"auth.jwks_fetch_failures_total", got.Auth.JWKSFetchFailures, before.Auth.JWKSFetchFailures + 1},
		{"catalog.presign_failures_total", got.Catalog.PresignFailures, before.Catalog.PresignFailures + 1},
		{"playback.now_playing_enrichment_failures_total", got.Playback.EnrichmentFailures, before.Playback.EnrichmentFailures + 1},
		{"playback.corrupt_stored_state_total", got.Playback.CorruptStoredState, before.Playback.CorruptStoredState + 1},
		{"playback.queue_state_op_timeouts_total", got.Playback.QueueStateOpTimeouts, before.Playback.QueueStateOpTimeouts + 1},
		{"playback.now_playing_lookup_timeouts_total", got.Playback.NowPlayingLookupTimeouts, before.Playback.NowPlayingLookupTimeouts + 1},
	} {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.key, c.got, c.want)
		}
	}
}

type statusTransport struct{ status int }

func (s statusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: s.status, Body: http.NoBody, Header: make(http.Header), Request: req}, nil
}

type providerWire struct {
	Providers map[string]struct {
		OK    int64 `json:"ok"`
		Quota int64 `json:"quota"`
	} `json:"providers"`
	Playback struct {
		Open       bool  `json:"now_playing_enrichment_breaker_open"`
		Rejections int64 `json:"now_playing_enrichment_breaker_rejections_total"`
	} `json:"playback"`
	Latency struct {
		Routes map[string]struct {
			Count  uint64            `json:"count"`
			Status map[string]uint64 `json:"status"`
		} `json:"routes"`
	} `json:"latency"`
}

func TestLive_CarriesProviderBreakerAndLatency(t *testing.T) {
	s := newSources(t).Sources
	var before providerWire
	decode(t, served(t, s), &before)

	const secret = "supersecretquery"
	for url, status := range map[string]int{
		"https://api.deezer.com/search?q=" + secret:     http.StatusOK,
		"https://api.spotify.com/v1/search?q=" + secret: http.StatusTooManyRequests,
	} {
		resp, err := search.NewCountingTransport(statusTransport{status}).RoundTrip(httptest.NewRequest(http.MethodGet, url, nil))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	pb := playback.NewExpvarPlaybackMetrics()
	pb.EnrichmentBreakerOpened()
	pb.EnrichmentBreakerRejected()
	defer pb.EnrichmentBreakerClosed()
	const route = "/v1/live-snapshot-probe/{status}"
	router := chi.NewRouter()
	router.Use(requestmetrics.Middleware(s.Latency.Observe))
	router.Get(route, func(w http.ResponseWriter, r *http.Request) {
		status, _ := strconv.Atoi(chi.URLParam(r, "status"))
		w.WriteHeader(status)
	})
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusBadGateway} {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/live-snapshot-probe/"+strconv.Itoa(status), nil))
	}

	raw := served(t, s)
	var got providerWire
	decode(t, raw, &got)
	if got.Providers["deezer"].OK != before.Providers["deezer"].OK+1 {
		t.Errorf("providers.deezer.ok = %d, want %d", got.Providers["deezer"].OK, before.Providers["deezer"].OK+1)
	}
	if got.Providers["spotify"].Quota != before.Providers["spotify"].Quota+1 {
		t.Errorf("providers.spotify.quota = %d, want %d", got.Providers["spotify"].Quota, before.Providers["spotify"].Quota+1)
	}
	if !got.Playback.Open || got.Playback.Rejections != before.Playback.Rejections+1 {
		t.Errorf("breaker open=%v rejections=%d, want open and %d", got.Playback.Open, got.Playback.Rejections, before.Playback.Rejections+1)
	}
	rl := got.Latency.Routes[route]
	if rl.Count != 3 || rl.Status["2xx"] != 1 || rl.Status["4xx"] != 1 || rl.Status["5xx"] != 1 {
		t.Errorf("latency.routes[%q] = %+v, want count 3 and one each of 2xx/4xx/5xx", route, rl)
	}
	if strings.Contains(string(raw), secret) {
		t.Errorf("snapshot leaks query text %q", secret)
	}
}
