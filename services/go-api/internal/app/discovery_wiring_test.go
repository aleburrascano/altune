package app_test

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/telemetry"
	"altune/go-api/internal/shared/timingtest"
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

type countingProviderRT struct {
	mu    sync.Mutex
	calls int
}

func (rt *countingProviderRT) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.calls++
	rt.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader("{}")),
		Request:    req,
	}, nil
}

func (rt *countingProviderRT) roundTrips() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.calls
}

func totalProviderCounts(s searchtest.ProviderMetricsSnapshot) int64 {
	var total int64
	for _, o := range s {
		total += o.OK + o.Quota + o.Error
	}
	return total
}

func TestDiscovery_RequestPathProviderCallsAreCountedOnce(t *testing.T) {
	routes := []struct {
		name   string
		target string
	}{
		{"album content", "/v1/discovery/albums/deezer/1/tracks"},
		{"lyrics", "/v1/discovery/lyrics?title=song&subtitle=artist"},
		{"enrichment", "/v1/discovery/enrichment?kind=album&title=album"},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			fixture := newJWKSFixture(t)
			fixture.cfg.MusicBrainzUserAgent = "altune-test/1.0"
			rt := &countingProviderRT{}
			reg := telemetry.NewRegistry()
			h := assembleWith(t, fixture.cfg, search.NewCountingTransport(rt, reg)).assembly.Handler()
			before, callsBefore := search.ReadProviderMetricsSnapshot(reg), rt.roundTrips()

			code, body := getAs(t, h, route.target, signedToken(t, fixture.key, fixture.operator))

			calls := rt.roundTrips() - callsBefore
			counted := totalProviderCounts(search.ReadProviderMetricsSnapshot(reg)) - totalProviderCounts(before)
			if calls == 0 {
				t.Fatalf("%s made no provider call (status %d, body %s), so it proves nothing about the transport", route.target, code, body)
			}
			if counted != int64(calls) {
				t.Errorf("provider counters moved by %d over %d provider calls, want one count per call", counted, calls)
			}
		})
	}
}

func TestDiscovery_AFailedBehavioralRankingRefreshIsLoggedAndCounted(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.BehavioralRankingEnabled = true
	run := assembleLogging(t, cfg)

	var h background.JobHealth
	timingtest.Within(3*time.Second, func() bool {
		h = jobHealth(t, run.assembly, jobs.BehavioralRankingRefresh)
		return h.Failures != 0
	})

	if h.Failures == 0 || h.LastFailure.IsZero() {
		t.Fatalf("ranking refresh health = %+v, want a counted failure with its time against an unreachable store", h)
	}
	if want := `level=WARN msg="behavioral ranking refresh failed" error=`; !strings.Contains(run.logs.String(), want) {
		t.Errorf("failure line drifted: want %q in\n%s", want, run.logs.String())
	}
}

func TestUnreleasedRefresh_AGeniusTokenStartsItEveryThirtyMinutes(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.GeniusAccessToken = "genius-token"

	want := `level=INFO msg="unreleased refresh started" interval=30m0s` + "\n"
	if logged := assembleLogging(t, cfg).logs.String(); !strings.Contains(logged, want) {
		t.Errorf("want %q in\n%s", want, logged)
	}
}

func TestUnreleasedRefresh_NoUnreleasedSourceStartsNoRefresh(t *testing.T) {
	if logged := assembleLogging(t, jwksConfig(t)).logs.String(); strings.Contains(logged, "unreleased refresh started") {
		t.Errorf("the unreleased refresh started without Genius or the tracker sheets configured:\n%s", logged)
	}
}

func TestNewTracker_RefusesACapBelowOne(t *testing.T) {
	_, err := app.NewTracker(&config.Config{})
	if !errors.Is(err, background.ErrTrackerLimit) {
		t.Fatalf("NewTracker = %v, want runloop.ErrTrackerLimit", err)
	}
}

func TestNewEvalDiscovery_RefusesAMissingTracker(t *testing.T) {
	_, err := app.NewEvalDiscovery(t.Context(), &config.Config{}, app.EvalHandles{Pool: unreachablePool(t), Transport: refuseEgress{}})
	if err == nil || !strings.Contains(err.Error(), "Tracker") {
		t.Fatalf("NewEvalDiscovery = %v, want an error naming the missing Tracker", err)
	}
}

func TestNewEvalDiscovery_RefusesAMissingTransport(t *testing.T) {
	_, err := app.NewEvalDiscovery(t.Context(), &config.Config{}, app.EvalHandles{Pool: unreachablePool(t), Tracker: testTracker(t)})
	if err == nil || !strings.Contains(err.Error(), "Transport") {
		t.Fatalf("NewEvalDiscovery = %v, want an error naming the missing Transport", err)
	}
}

func TestNewEvalDiscovery_SearchesThroughTheGivenTransport(t *testing.T) {
	logs := logging.CaptureText(t, slog.LevelInfo)

	disc, err := app.NewEvalDiscovery(t.Context(), &config.Config{}, app.EvalHandles{Pool: unreachablePool(t), Transport: refuseEgress{}, Tracker: testTracker(t)})
	if err != nil {
		t.Fatalf("NewEvalDiscovery: %v", err)
	}
	query := &search.SearchQuery{Raw: "probe song", Kinds: map[search.ResultKind]bool{search.ResultKindAlbum: true}}
	disc.RankingOnlySearch().InspectSearchWithStatuses(t.Context(), query)
	if !strings.Contains(logs.String(), "test transport: provider egress refused") {
		t.Errorf("a ranking search never went through the given transport; logs:\n%s", logs.String())
	}
}

type recordedClientEvents struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (r *recordedClientEvents) OnEmit(_ context.Context, rec *sdklog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec.Clone())
	return nil
}

func (r *recordedClientEvents) Enabled(context.Context, sdklog.EnabledParameters) bool { return true }

func (r *recordedClientEvents) Shutdown(context.Context) error { return nil }

func (r *recordedClientEvents) ForceFlush(context.Context) error { return nil }

func (r *recordedClientEvents) exported(eventID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rec := range r.records {
		found := false
		rec.WalkAttributes(func(kv attribute.KeyValue) bool {
			found = kv.Key == "event.id" && kv.Value.AsString() == eventID
			return !found
		})
		if found && rec.Body().AsString() == "client_event" {
			return true
		}
	}
	return false
}

func recordClientEvents(t *testing.T) *recordedClientEvents {
	t.Helper()
	recorded := &recordedClientEvents{}
	prev := otel.GetLoggerProvider()
	otel.SetLoggerProvider(sdklog.NewLoggerProvider(sdklog.WithProcessor(recorded)))
	t.Cleanup(func() { otel.SetLoggerProvider(prev) })
	return recorded
}

func eventStreamLines(t *testing.T, srv *httptest.Server, token string) <-chan string {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/observe/events/stream", nil)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("open the event stream: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("event stream = %d, want 200", resp.StatusCode)
	}
	lines := make(chan string)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	if first := <-lines; first != ": keepalive" {
		t.Fatalf("first stream line %q, want the keepalive that follows subscribing", first)
	}
	return lines
}

func postScreenView(t *testing.T, srv *httptest.Server, token, eventID string) {
	t.Helper()
	body := `{"type":"screen_view","event_id":"` + eventID + `"}`
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/v1/discovery/events", strings.NewReader(body))
	if err != nil {
		t.Fatalf("event request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("post the event: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /v1/discovery/events = %d, want 204", resp.StatusCode)
	}
}

func TestDiscovery_AStoredClientEventReachesTheObserveStreamAndTheTelemetryExport(t *testing.T) {
	recorded := recordClientEvents(t)
	f := newJWKSFixture(t)
	f.cfg.OTLPEndpoint = "https://otlp.example.com"
	srv := httptest.NewServer(assembledRouter(t, f.cfg, dbtest.Pool(t)))
	t.Cleanup(srv.Close)
	token := signedToken(t, f.key, f.operator)
	lines := eventStreamLines(t, srv, token)
	eventID := uuid.NewString()

	postScreenView(t, srv, token, eventID)

	timeout := time.After(10 * time.Second)
	for streamed := false; !streamed; {
		select {
		case line, open := <-lines:
			if !open {
				t.Fatal("the event stream closed before the stored event reached it")
			}
			streamed = strings.HasPrefix(line, "data: ") && strings.Contains(line, `"screen_view"`)
		case <-timeout:
			t.Fatal("the stored screen_view never reached /observe/events/stream: discovery's activity feed is not the assembly's event tap")
		}
	}
	if !recorded.exported(eventID) {
		t.Errorf("no client_event with event.id %s was exported, although an OTLP endpoint is configured", eventID)
	}
}
