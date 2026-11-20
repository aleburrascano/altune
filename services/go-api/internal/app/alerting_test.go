package app

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/observe"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestBuildCoverageCondition(t *testing.T) {
	ctx := context.Background()

	t.Run("fires on the true total even when the listed queries sum below the threshold", func(t *testing.T) {
		events := &fakeCoverageEvents{topN: []discovery.QueryCount{{QueryNorm: "q", Count: 1}}, total: 25}
		gap, _ := buildCoverageConditions(events)
		if alert := gap.Eval(ctx); alert == nil {
			t.Fatalf("alert did not fire: true total %d meets the threshold but the listed queries sum to 1", events.total)
		}
	})

	t.Run("24 zero-result searches do not fire", func(t *testing.T) {
		events := &fakeCoverageEvents{topN: []discovery.QueryCount{{QueryNorm: "rare", Count: 3}}, total: 24}
		gap, _ := buildCoverageConditions(events)
		if alert := gap.Eval(ctx); alert != nil {
			t.Fatalf("alert fired with total %d below the threshold of 25: %+v", events.total, alert)
		}
	})

	t.Run("25 zero-result searches fire", func(t *testing.T) {
		events := &fakeCoverageEvents{topN: []discovery.QueryCount{{QueryNorm: "missing band", Count: 20}}, total: 25}
		gap, _ := buildCoverageConditions(events)
		alert := gap.Eval(ctx)
		if alert == nil {
			t.Fatal("alert did not fire at total == 25")
		}
		if alert.Title != "altune discovery coverage gap" {
			t.Errorf("title = %q", alert.Title)
		}
	})
}

func TestBuildCoverageCondition_MessageExcludesQueryText(t *testing.T) {
	const query = "my private search \"quoted\" term"
	events := &fakeCoverageEvents{
		topN:  []discovery.QueryCount{{QueryNorm: query, Count: 42}},
		total: 50,
	}
	gap, _ := buildCoverageConditions(events)
	alert := gap.Eval(context.Background())
	if alert == nil {
		t.Fatal("alert did not fire above threshold")
	}
	for _, fragment := range []string{query, "private", "quoted"} {
		if strings.Contains(alert.Message, fragment) || strings.Contains(alert.Title, fragment) {
			t.Fatalf("alert leaks query text %q: title=%q message=%q", fragment, alert.Title, alert.Message)
		}
	}
	for _, want := range []string{"50", "25", "42"} {
		if !strings.Contains(alert.Message, want) {
			t.Errorf("message %q lost count %s", alert.Message, want)
		}
	}
}

func evalTick(ctx context.Context, conds ...observe.Condition) map[string]*observe.Alert {
	fired := make(map[string]*observe.Alert)
	for _, c := range conds {
		if a := c.Eval(ctx); a != nil {
			fired[c.Key] = a
		}
	}
	return fired
}

func TestBuildCoverageConditions_QueryFailureIsNotHealthy(t *testing.T) {
	ctx := context.Background()

	t.Run("persistent failure escalates to its own signal", func(t *testing.T) {
		events := &fakeCoverageEvents{totalErr: errors.New("relation zero_result does not exist")}
		gap, queryFailing := buildCoverageConditions(events)

		for i := 1; i < coverageQueryFailureEscalation; i++ {
			if fired := evalTick(ctx, gap, queryFailing); len(fired) != 0 {
				t.Fatalf("failure %d fired %v before escalation threshold %d", i, fired, coverageQueryFailureEscalation)
			}
		}
		alert := evalTick(ctx, gap, queryFailing)[queryFailing.Key]
		if alert == nil {
			t.Fatalf("%d consecutive query failures resolved to healthy; want a failure signal", coverageQueryFailureEscalation)
		}
		if queryFailing.Key == gap.Key {
			t.Errorf("failure signal shares key %q with the gap alert", gap.Key)
		}
		if alert.Severity != observe.SeveritySignal {
			t.Errorf("severity = %v, want SeveritySignal", alert.Severity)
		}
		if strings.Contains(alert.Message, "relation") || strings.Contains(alert.Message, "zero_result") {
			t.Errorf("failure message leaks the driver error: %q", alert.Message)
		}
	})

	t.Run("failure while a gap is firing does not resolve the gap", func(t *testing.T) {
		events := &fakeCoverageEvents{total: 50}
		gap, queryFailing := buildCoverageConditions(events)
		if evalTick(ctx, gap, queryFailing)[gap.Key] == nil {
			t.Fatal("gap did not fire above threshold")
		}
		events.totalErr = errors.New("conn reset")
		if evalTick(ctx, gap, queryFailing)[gap.Key] == nil {
			t.Fatal("a query failure resolved a firing gap alert as healthy")
		}
	})

	t.Run("a successful query resets the failure streak", func(t *testing.T) {
		events := &fakeCoverageEvents{totalErr: errors.New("timeout")}
		gap, queryFailing := buildCoverageConditions(events)
		for range coverageQueryFailureEscalation {
			evalTick(ctx, gap, queryFailing)
		}
		events.totalErr = nil
		events.total = 1
		if fired := evalTick(ctx, gap, queryFailing); len(fired) != 0 {
			t.Fatalf("fired %v after the query recovered below threshold", fired)
		}
	})
}

func TestBuildDependencyCondition(t *testing.T) {
	ctx := context.Background()
	up := observe.DependencyHealth{DB: observe.DepUp, Redis: observe.DepUp, Auth: observe.DepUp}

	cases := []struct {
		name    string
		health  observe.DependencyHealth
		wantMsg string
	}{
		{"auth only down names auth", observe.DependencyHealth{DB: observe.DepUp, Redis: observe.DepUp, Auth: observe.DepDown}, "dependencies down: auth"},
		{"db only down names db", observe.DependencyHealth{DB: observe.DepDown, Redis: observe.DepUp, Auth: observe.DepUp}, "dependencies down: db"},
		{"redis only down names redis", observe.DependencyHealth{DB: observe.DepUp, Redis: observe.DepDown, Auth: observe.DepUp}, "dependencies down: redis"},
		{"all down names all", observe.DependencyHealth{DB: observe.DepDown, Redis: observe.DepDown, Auth: observe.DepDown}, "dependencies down: db redis auth"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cond := buildDependencyCondition(func(context.Context) observe.DependencyHealth { return tc.health })
			if cond.Key != "dependency_down" {
				t.Fatalf("key = %q, want dependency_down", cond.Key)
			}
			alert := cond.Eval(ctx)
			if alert == nil {
				t.Fatal("alert = nil, want it to fire")
			}
			if alert.Message != tc.wantMsg {
				t.Fatalf("message = %q, want %q", alert.Message, tc.wantMsg)
			}
		})
	}

	t.Run("healthy does not fire", func(t *testing.T) {
		cond := buildDependencyCondition(func(context.Context) observe.DependencyHealth { return up })
		if alert := cond.Eval(ctx); alert != nil {
			t.Fatalf("alert = %+v, want nil", alert)
		}
	})
}

func TestJobFailingCondition(t *testing.T) {
	ctx := context.Background()
	a := &App{}
	name := jobDeletedIdentityErasure
	jc := a.job(name)
	cond := buildJobCondition(name, jc)

	if cond.Key != "job_failing:deleted identity erasure" {
		t.Fatalf("key = %q", cond.Key)
	}

	for range jobFailureEscalation - 1 {
		jc.Record(errors.New("boom secret"))
	}
	if got := cond.Eval(ctx); got != nil {
		t.Fatalf("fired below escalation: %+v", got)
	}

	jc.Record(errors.New("boom secret"))
	got := cond.Eval(ctx)
	if got == nil {
		t.Fatal("consecutive failures produced no alert")
	}
	if strings.Contains(got.Message, "boom") || !strings.Contains(got.Message, string(name)) {
		t.Fatalf("message = %q, want job name and no error text", got.Message)
	}

	if _, ok := a.SetJobEnabled(name, false); !ok {
		t.Fatal("job not registered")
	}
	if got := cond.Eval(ctx); got != nil {
		t.Fatalf("disabled job fired: %+v", got)
	}
	a.SetJobEnabled(name, true)

	jc.Record(nil)
	if got := cond.Eval(ctx); got != nil {
		t.Fatalf("recovered job still firing: %+v", got)
	}
}

func TestStartAlertMonitor_RegistersToStartWhenLeader(t *testing.T) {
	a := &App{cfg: &config.Config{}}
	a.startAlertMonitor(context.Background())

	registered := false
	for _, job := range a.backgroundStarts {
		registered = registered || job.name == jobAlertMonitor
	}
	if !registered {
		t.Error("the alert monitor is not registered to start when leader")
	}
}

func TestStartAlertMonitor_PassesEvaluateItsConditions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var probes atomic.Int64
		a := &App{cfg: &config.Config{}}
		a.dbHealth = func(context.Context) database.HealthStatus {
			probes.Add(1)
			return database.HealthStatus{OK: true}
		}
		ctx, cancel := context.WithCancel(t.Context())
		a.startAlertMonitor(ctx)
		for _, job := range a.backgroundStarts {
			job.start(ctx)
		}

		time.Sleep(31 * time.Second)
		synctest.Wait()

		if probes.Load() == 0 {
			t.Error("a monitor pass ran without probing the database: the dependency condition never reached the monitor")
		}
		cancel()
		a.alertMonitor.Shutdown(context.Background())
	})
}

func TestAlertConditions_AlwaysWatchCoverage(t *testing.T) {
	a := &App{cfg: &config.Config{}}
	conditions := a.alertConditions(&fakeCoverageEvents{total: 25})

	byKey := map[string]observe.Condition{}
	for _, c := range conditions {
		byKey[c.Key] = c
	}
	want := []string{"dependency_down", "coverage_zero_result", "coverage_query_failing"}
	for _, name := range alertableJobs {
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

	t.Run("webhook url set sends alerts to that webhook", func(t *testing.T) {
		var posts atomic.Int32
		hook := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { posts.Add(1) }))
		defer hook.Close()

		a := &App{cfg: &config.Config{AlertWebhookURL: hook.URL}}
		if err := a.alertNotifier(context.Background()).Notify(context.Background(), observe.Alert{Title: "t"}); err != nil {
			t.Fatalf("Notify = %v, want nil", err)
		}
		if posts.Load() != 1 {
			t.Fatalf("webhook got %d posts, want 1", posts.Load())
		}
	})

	t.Run("email settings build a real notifier without the log-only warning", func(t *testing.T) {
		var buf bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
		t.Cleanup(func() { slog.SetDefault(prev) })

		a := &App{cfg: &config.Config{ResendAPIKey: "re_TESTKEY_never_in_output", AlertEmailTo: "ops@example.com"}}
		a.startAlertMonitor(context.Background())

		if a.alertMonitor.Status().NopNotifier {
			t.Fatal("monitor kept NopNotifier although RESEND_API_KEY and ALERT_EMAIL_TO are set")
		}
		if strings.Contains(buf.String(), "log-only") || strings.Contains(buf.String(), "TESTKEY") {
			t.Fatalf("startup log warns log-only or carries the key: %s", buf.String())
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

func TestJobFailingCondition_UnreadableIdentityStoreFiresAfterEscalation(t *testing.T) {
	ctx := context.Background()
	a := &App{}
	name := jobDeletedIdentityErasure
	jc := a.job(name)
	cond := buildJobCondition(name, jc)
	sweepErr := fmt.Errorf("deleted identity sweep idle: %w", playback.ErrIdentityStoreUnavailable)

	for range jobFailureEscalation {
		jc.Record(sweepErr)
	}

	if got := cond.Eval(ctx); got == nil {
		t.Fatal("unreadable identity store for consecutive runs produced no alert")
	}
}

func TestAlertableJobs_PinnedInOrder(t *testing.T) {
	want := []jobName{
		jobOrphanedAudioReconcile,
		jobBehavioralCorpusRefresh,
		jobDiscographyEventPrune,
		jobVocabularyRefresh,
		jobBehavioralRankingRefresh,
		jobDeletedIdentityErasure,
		jobAcquisitionSourceCanary,
		jobAcquisitionPrune,
		jobAcquisitionUpgrade,
		jobUnreleasedRefresh,
	}
	if !reflect.DeepEqual(alertableJobs, want) {
		t.Fatalf("alertableJobs = %v, want %v", alertableJobs, want)
	}
}

func TestAlertableJobs_ExclusionsAreExplicit(t *testing.T) {
	excluded := map[jobName]bool{jobStreamRecovery: true}
	alertable := map[jobName]bool{}
	for _, name := range alertableJobs {
		alertable[name] = true
	}
	for _, name := range allJobNameConsts {
		if alertable[name] == excluded[name] {
			t.Fatalf("job %q alertable=%v excluded=%v: exactly one must hold", name, alertable[name], excluded[name])
		}
	}
}
