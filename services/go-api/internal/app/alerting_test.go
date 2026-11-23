package app

import (
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"altune/go-api/internal/shared/config"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type fakeCoverageEvents struct {
	topN     []discovery.QueryCount
	total    int
	totalErr error
}

func (f *fakeCoverageEvents) ZeroResultTotal(context.Context, time.Time) (int, error) {
	if f.totalErr != nil {
		return 0, f.totalErr
	}
	return f.total, nil
}

func (f *fakeCoverageEvents) ZeroResultQueries(_ context.Context, _ time.Time, limit int) ([]discovery.QueryCount, error) {
	if len(f.topN) > limit {
		return f.topN[:limit], nil
	}
	return f.topN, nil
}

func TestAlertConditions_WatchTheConfiguredCookieJar(t *testing.T) {
	jar := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(jar, []byte("# Netscape HTTP Cookie File\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 11, 20, 14, 30, 0, 0, time.UTC)
	session := fmt.Sprintf(`{"jar_sha256":"%x","signed_out_since":"%s"}`, sha256.Sum256([]byte("# Netscape HTTP Cookie File\n")), since.Format(time.RFC3339))
	if err := os.WriteFile(jar+".session.json", []byte(session), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &App{cfg: &config.Config{YtDLPCookieFile: jar}}

	for _, c := range a.alertConditions(&fakeCoverageEvents{}) {
		if c.Key != "youtube_cookies_signed_out" {
			continue
		}
		if alert := c.Eval(context.Background()); alert == nil || !strings.Contains(alert.Message, "since 2026-11-20T14:30:00Z") {
			t.Errorf("alert = %+v, want it to fire from the configured jar's session", alert)
		}
		return
	}
	t.Fatal("no youtube_cookies_signed_out condition")
}

func TestStartAlertMonitor_PassesEvaluateItsConditions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var probes atomic.Int64
		a := &App{cfg: &config.Config{}}
		a.dbPing = func(context.Context) error {
			probes.Add(1)
			return nil
		}
		ctx, cancel := context.WithCancel(t.Context())
		a.startAlertMonitor(ctx)
		a.jobs.Lead(ctx, &leaderElection{})

		time.Sleep(31 * time.Second)
		synctest.Wait()

		if probes.Load() == 0 {
			t.Error("a monitor pass ran without probing the database: the dependency condition never reached the monitor")
		}
		cancel()
		a.alertMonitor.Shutdown(context.Background())
		a.jobs.Background().Wait()
	})
}

func TestAlertConditions_AlwaysWatchCoverage(t *testing.T) {
	a := &App{cfg: &config.Config{}}
	conditions := a.alertConditions(&fakeCoverageEvents{total: 25})

	byKey := map[string]observe.Condition{}
	for _, c := range conditions {
		byKey[c.Key] = c
	}
	want := []string{"dependency_down", "coverage_zero_result", "coverage_query_failing", "youtube_cookies_signed_out"}
	for _, name := range jobs.Alertable() {
		want = append(want, "job_failing:"+string(name))
	}
	if len(conditions) != len(want) {
		t.Fatalf("got %d conditions, want %d: %v", len(conditions), len(want), want)
	}
	for _, key := range want {
		if _, ok := byKey[key]; !ok {
			t.Errorf("condition %q missing", key)
		}
	}
	if byKey["coverage_zero_result"].Eval(context.Background()) == nil {
		t.Error("the coverage condition does not read the events it was given")
	}
}

func TestStartAlertMonitor_NotifierFollowsAlertConfig(t *testing.T) {
	t.Run("webhook url set builds a real notifier", func(t *testing.T) {
		a := &App{cfg: &config.Config{AlertWebhookURL: "http://127.0.0.1:1/alerts"}}
		a.startAlertMonitor(context.Background())
		if a.alertMonitor.Status().NopNotifier {
			t.Fatal("monitor kept NopNotifier although ALERT_WEBHOOK_URL is set")
		}
	})

	t.Run("no alert channel warns once that alerts are log-only", func(t *testing.T) {
		var buf bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
		t.Cleanup(func() { slog.SetDefault(prev) })

		a := &App{cfg: &config.Config{}}
		a.startAlertMonitor(context.Background())

		if !a.alertMonitor.Status().NopNotifier {
			t.Fatal("monitor has a real notifier without any alert channel")
		}
		if n := strings.Count(buf.String(), "alerts are log-only"); n != 1 {
			t.Fatalf("log-only warnings = %d, want 1; log: %s", n, buf.String())
		}
		if !strings.Contains(buf.String(), "level=WARN") {
			t.Fatalf("log-only notice is not a Warn: %s", buf.String())
		}
	})
}
