package app_test

import (
	"altune/overseer/internal/app"
	"altune/overseer/internal/config"
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func noEnv(string) string { return "" }

func envWith(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func built(t *testing.T, getenv func(string) string, id string) core.Bucket {
	t.Helper()
	for _, b := range app.Buckets(&config.Config{}, getenv) {
		if b.Meta().ID == id {
			return b
		}
	}
	t.Fatalf("app.Buckets builds no %q bucket", id)
	return nil
}

func TestTheOverseerBuildsExactlyItsNineBuckets(t *testing.T) {
	var ids []string
	for _, b := range app.Buckets(&config.Config{}, noEnv) {
		ids = append(ids, b.Meta().ID)
	}
	slices.Sort(ids)

	want := []string{"backendperf", "cost", "domainquality", "heartbeat", "liveactivity", "logs", "reliability", "security", "usage"}
	if !slices.Equal(ids, want) {
		t.Fatalf("app.Buckets() = %v, want exactly %v", ids, want)
	}
}

func TestTheGoAPIURLReachesTheEventStreams(t *testing.T) {
	for _, id := range []string{"liveactivity", "usage"} {
		t.Run(id, func(t *testing.T) {
			configured := built(t, envWith(map[string]string{"OVERSEER_GOAPI_URL": " http://127.0.0.1:1 "}), id)
			if state := configured.Snapshot().State; state == core.StateSourceDown {
				t.Fatalf("%s with OVERSEER_GOAPI_URL set is %q, want a stream that is connecting", id, state)
			}

			unconfigured := built(t, noEnv, id)
			if state := unconfigured.Snapshot().State; state != core.StateSourceDown {
				t.Fatalf("%s with no OVERSEER_GOAPI_URL is %q, want source_down", id, state)
			}

			invalid := built(t, envWith(map[string]string{"OVERSEER_GOAPI_URL": "://not a url"}), id)
			if state := invalid.Snapshot().State; state != core.StateSourceDown {
				t.Fatalf("%s with an invalid OVERSEER_GOAPI_URL is %q, want source_down", id, state)
			}
		})
	}
}

func TestTheLogsMinimumLevelReachesTheLogsBucket(t *testing.T) {
	b := built(t, envWith(map[string]string{"OVERSEER_LOGS_MIN_LEVEL": "WARN"}), "logs")

	var data struct {
		MinLevel string `json:"minLevel"`
	}
	if err := json.Unmarshal(b.Snapshot().Data, &data); err != nil {
		t.Fatalf("decode logs snapshot: %v", err)
	}
	if data.MinLevel != "WARN" {
		t.Fatalf("logs minLevel = %q, want WARN from OVERSEER_LOGS_MIN_LEVEL", data.MinLevel)
	}
}

func TestTheGoAPIURLReachesTheLogsStream(t *testing.T) {
	configured := built(t, envWith(map[string]string{"OVERSEER_GOAPI_URL": "http://127.0.0.1:1"}), "logs")
	if state := configured.Snapshot().State; state == core.StateSourceDown {
		t.Fatalf("logs with OVERSEER_GOAPI_URL set is %q, want a stream that is connecting", state)
	}
	if state := built(t, noEnv, "logs").Snapshot().State; state != core.StateSourceDown {
		t.Fatalf("logs with no OVERSEER_GOAPI_URL is %q, want source_down", state)
	}
}

func collectErr(t *testing.T, b core.Bucket) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := b.Collect(ctx)
	return err
}

func TestTheGoAPIURLReachesTheClients(t *testing.T) {
	for _, id := range []string{"backendperf", "cost", "domainquality", "reliability"} {
		t.Run(id, func(t *testing.T) {
			configured := built(t, envWith(map[string]string{"OVERSEER_GOAPI_URL": " http://127.0.0.1:1 "}), id)
			if err := collectErr(t, configured); err != nil && strings.Contains(err.Error(), "not configured") {
				t.Fatalf("%s with OVERSEER_GOAPI_URL set collects %v, want a go-api client", id, err)
			}

			for name, getenv := range map[string]func(string) string{
				"unset":   noEnv,
				"blank":   envWith(map[string]string{"OVERSEER_GOAPI_URL": "  \t"}),
				"invalid": envWith(map[string]string{"OVERSEER_GOAPI_URL": "://not a url"}),
			} {
				err := collectErr(t, built(t, getenv, id))
				if err == nil || !strings.Contains(err.Error(), "not configured") {
					t.Fatalf("%s with an %s OVERSEER_GOAPI_URL collects %v, want the not-configured error", id, name, err)
				}
			}
		})
	}
}

type healthServer struct {
	*httptest.Server
	hits atomic.Int64
}

func newHealthServer(t *testing.T) *healthServer {
	t.Helper()
	s := &healthServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		s.hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(s.Close)
	return s
}

func started(t *testing.T, b core.Bucket) {
	t.Helper()
	runner, ok := b.(interface {
		Start(ctx context.Context)
		Wait()
	})
	if !ok {
		t.Fatalf("%s has no poller to start", b.Meta().ID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		runner.Wait()
	})
	runner.Start(ctx)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func reachability(t *testing.T, b core.Bucket) string {
	t.Helper()
	var data struct {
		Reachability string `json:"reachability"`
	}
	if err := json.Unmarshal(b.Snapshot().Data, &data); err != nil {
		t.Fatalf("decode reliability snapshot: %v", err)
	}
	return data.Reachability
}

func TestTheGoAPIURLReachesTheReliabilityProbe(t *testing.T) {
	srv := newHealthServer(t)
	b := built(t, envWith(map[string]string{"OVERSEER_GOAPI_URL": " " + srv.URL + " "}), "reliability")

	started(t, b)

	waitFor(t, "the probe of go-api /health", func() bool { return reachability(t, b) == "up" })
}

func TestTheReliabilityPollIntervalReachesThePoller(t *testing.T) {
	srv := newHealthServer(t)
	b := built(t, envWith(map[string]string{
		"OVERSEER_GOAPI_URL":                 srv.URL,
		"OVERSEER_RELIABILITY_POLL_INTERVAL": " 1ms ",
	}), "reliability")

	started(t, b)

	waitFor(t, "probes at the configured 1ms cadence", func() bool { return srv.hits.Load() >= 3 })
}

func TestAnUnsetReliabilityPollIntervalKeepsTheDefaultCadence(t *testing.T) {
	logs := capturedLogs(t)
	srv := newHealthServer(t)
	b := built(t, envWith(map[string]string{"OVERSEER_GOAPI_URL": srv.URL}), "reliability")

	started(t, b)
	waitFor(t, "the first probe", func() bool { return srv.hits.Load() == 1 })
	time.Sleep(50 * time.Millisecond)

	if got := srv.hits.Load(); got != 1 {
		t.Fatalf("with no OVERSEER_RELIABILITY_POLL_INTERVAL the poller probed %d times in 50ms, want 1", got)
	}
	if strings.Contains(logs.String(), "OVERSEER_RELIABILITY_POLL_INTERVAL") {
		t.Fatalf("an unset interval logged %s, want no warning", logs.String())
	}
}

func TestAnInvalidReliabilityPollIntervalWarns(t *testing.T) {
	for _, raw := range []string{"garbage", "0s", "-5s"} {
		t.Run(raw, func(t *testing.T) {
			logs := capturedLogs(t)

			built(t, envWith(map[string]string{"OVERSEER_RELIABILITY_POLL_INTERVAL": " " + raw + " "}), "reliability")

			if value, ok := warnedValue(t, logs, "reliability: invalid OVERSEER_RELIABILITY_POLL_INTERVAL, using default"); !ok || value != raw {
				t.Fatalf("logs = %s, want the invalid-interval warning carrying value %q", logs.String(), raw)
			}
		})
	}
}

func TestAnInvalidGoAPIURLWarnsForEachBucketInItsOwnWords(t *testing.T) {
	logs := capturedLogs(t)

	app.Buckets(&config.Config{}, envWith(map[string]string{"OVERSEER_GOAPI_URL": "://not a url"}))

	for _, want := range []string{
		"liveactivity: invalid OVERSEER_GOAPI_URL, degrading to source-down",
		"logs: invalid OVERSEER_GOAPI_URL, degrading to source-down",
		"usage: invalid OVERSEER_GOAPI_URL, degrading to source-down",
		"backendperf: invalid OVERSEER_GOAPI_URL, degrading to source-down",
		"domainquality: invalid OVERSEER_GOAPI_URL, degrading to source-down",
		"cost: invalid OVERSEER_GOAPI_URL, degrading provider-usage to source-down",
		"reliability: invalid OVERSEER_GOAPI_URL, degrading to source-down",
	} {
		if _, ok := warnedValue(t, logs, want); !ok {
			t.Errorf("logs = %s, want the warning %q", logs.String(), want)
		}
	}
}

func capturedLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func warnedValue(t *testing.T, logs *bytes.Buffer, msg string) (string, bool) {
	t.Helper()
	for line := range strings.Lines(logs.String()) {
		var entry struct {
			Level string `json:"level"`
			Msg   string `json:"msg"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		if entry.Level == "WARN" && entry.Msg == msg {
			return entry.Value, true
		}
	}
	return "", false
}

type selfTest struct {
	HasRun  bool          `json:"hasRun"`
	Passed  int           `json:"passed"`
	History []core.Signal `json:"history"`
}

func rejectingGoAPI(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func hostOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u.Host
}

func startSecurity(t *testing.T, getenv func(string) string) core.Bucket {
	t.Helper()
	b := built(t, getenv, "security")
	starter, ok := b.(core.Starter)
	if !ok {
		t.Fatal("security is not a core.Starter, so its self-test never runs")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	starter.Start(ctx)
	return b
}

func selfTestOf(t *testing.T, b core.Bucket) selfTest {
	t.Helper()
	var d selfTest
	if err := json.Unmarshal(b.Snapshot().Data, &d); err != nil {
		t.Fatalf("decode security snapshot: %v", err)
	}
	return d
}

func waitForSelfTestPass(t *testing.T, b core.Bucket) {
	t.Helper()
	waitFor(t, "a self-test run that reached go-api and passed 4/4", func() bool { d := selfTestOf(t, b); return d.HasRun && d.Passed == 4 })
}

func waitForSourceDown(t *testing.T, b core.Bucket) {
	t.Helper()
	waitFor(t, "the self-test to degrade to source-down", func() bool { return b.Snapshot().Reason == goapi.ReasonDown })
	if snap := b.Snapshot(); snap.State != core.StateSourceDown || selfTestOf(t, b).HasRun {
		t.Fatalf("security state %q, want source_down with no reachable run", snap.State)
	}
}

type logRecords struct {
	buf bytes.Buffer
}

func captureLogs(t *testing.T) *logRecords {
	t.Helper()
	c := &logRecords{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&c.buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return c
}

func (c *logRecords) records(msg string) []map[string]any {
	var out []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(c.buf.Bytes()), []byte("\n")) {
		var rec map[string]any
		if json.Unmarshal(line, &rec) == nil && rec["msg"] == msg {
			out = append(out, rec)
		}
	}
	return out
}

const (
	fencedClientWarning     = "security: cannot build fenced client, degrading to source-down"
	securityIntervalWarning = "security: invalid OVERSEER_SECURITY_INTERVAL, using default"
)

func TestTheGoAPIURLReachesTheSecuritySelfTest(t *testing.T) {
	srv, hits := rejectingGoAPI(t)

	b := startSecurity(t, envWith(map[string]string{"OVERSEER_GOAPI_URL": " " + srv.URL + "/ "}))

	waitForSelfTestPass(t, b)
	if hits.Load() == 0 {
		t.Fatal("the self-test passed without a probe reaching OVERSEER_GOAPI_URL")
	}
}

func TestTheSecuritySelfTestIsSourceDownWithoutAGoAPIURL(t *testing.T) {
	logs := captureLogs(t)

	waitForSourceDown(t, startSecurity(t, noEnv))

	if got := logs.records(fencedClientWarning); len(got) != 0 {
		t.Fatalf("no OVERSEER_GOAPI_URL logged %v, want silence: unconfigured is not a broken fence", got)
	}
}

func TestTheSecurityAllowlistReachesTheFence(t *testing.T) {
	srv, _ := rejectingGoAPI(t)
	host := hostOf(t, srv.URL)

	for name, allow := range map[string]string{
		"blank defaults to the base": "  ",
		"listed among others":        "other.test," + host,
	} {
		t.Run(name, func(t *testing.T) {
			b := startSecurity(t, envWith(map[string]string{"OVERSEER_GOAPI_URL": srv.URL, "OVERSEER_SECURITY_ALLOWLIST": allow}))
			waitForSelfTestPass(t, b)
		})
	}
}

func TestASecurityAllowlistWithoutTheBaseFailsClosed(t *testing.T) {
	srv, hits := rejectingGoAPI(t)
	logs := captureLogs(t)

	b := startSecurity(t, envWith(map[string]string{"OVERSEER_GOAPI_URL": srv.URL, "OVERSEER_SECURITY_ALLOWLIST": "other.test, evil.test"}))

	waitForSourceDown(t, b)
	if hits.Load() != 0 {
		t.Fatalf("an off-allowlist base was probed %d time(s)", hits.Load())
	}
	got := logs.records(fencedClientWarning)
	if len(got) != 1 || !strings.Contains(got[0]["error"].(string), "not on allowlist") {
		t.Fatalf("warnings = %v, want one fenced-client warning naming the allowlist", got)
	}
}

func TestAnInvalidGoAPIURLLeavesSecuritySourceDown(t *testing.T) {
	logs := captureLogs(t)

	waitForSourceDown(t, startSecurity(t, envWith(map[string]string{"OVERSEER_GOAPI_URL": "ftp://api.test"})))

	if got := logs.records(fencedClientWarning); len(got) != 1 {
		t.Fatalf("warnings = %v, want one fenced-client warning", got)
	}
}

func TestTheSecurityIntervalReachesTheScheduler(t *testing.T) {
	srv, _ := rejectingGoAPI(t)

	b := startSecurity(t, envWith(map[string]string{"OVERSEER_GOAPI_URL": srv.URL, "OVERSEER_SECURITY_INTERVAL": " 1ms "}))

	waitFor(t, "a second run one millisecond after the first", func() bool { return len(selfTestOf(t, b).History) >= 2 })
}

func TestAnInvalidSecurityIntervalWarnsAndRunsHourly(t *testing.T) {
	for _, raw := range []string{"soon", "0s", "-1m"} {
		t.Run(raw, func(t *testing.T) {
			srv, _ := rejectingGoAPI(t)
			logs := captureLogs(t)

			b := startSecurity(t, envWith(map[string]string{"OVERSEER_GOAPI_URL": srv.URL, "OVERSEER_SECURITY_INTERVAL": raw}))

			got := logs.records(securityIntervalWarning)
			if len(got) != 1 || got[0]["value"] != raw {
				t.Fatalf("warnings = %v, want one naming value %q", got, raw)
			}
			waitForSelfTestPass(t, b)
			time.Sleep(50 * time.Millisecond)
			if n := len(selfTestOf(t, b).History); n != 1 {
				t.Fatalf("ran %d times within 50ms, want the hourly default", n)
			}
		})
	}
}

func TestAnUnsetSecurityIntervalIsSilent(t *testing.T) {
	logs := captureLogs(t)

	built(t, noEnv, "security")

	if got := logs.records(securityIntervalWarning); len(got) != 0 {
		t.Fatalf("unset OVERSEER_SECURITY_INTERVAL logged %v, want silence", got)
	}
}
