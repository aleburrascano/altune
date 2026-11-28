package service_test

import (
	"altune/go-api/internal/platform/monitoring/internal/domain"
	"altune/go-api/internal/platform/monitoring/internal/service"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	coverageQueryFailureEscalation = 3
	jobFailureEscalation           = 3
)

type queryCount struct {
	QueryNorm string
	Count     int
}

type fakeCoverageEvents struct {
	topN     []queryCount
	total    int
	totalErr error
	since    time.Time
}

func (f *fakeCoverageEvents) ZeroResultTotal(_ context.Context, since time.Time) (int, error) {
	f.since = since
	if f.totalErr != nil {
		return 0, f.totalErr
	}
	return f.total, nil
}

func (f *fakeCoverageEvents) TopNoResultQueryHits(context.Context, time.Time) (int, bool) {
	if len(f.topN) == 0 {
		return 0, false
	}
	return f.topN[0].Count, true
}

type fakeSessions struct {
	since     time.Time
	signedOut bool
}

func (f fakeSessions) YouTubeSignedOutSince(context.Context) (time.Time, bool) {
	return f.since, f.signedOut
}

type fakeRequests struct{ latency func() domain.RequestLatency }

func (f fakeRequests) RequestLatency() domain.RequestLatency {
	if f.latency == nil {
		return domain.RequestLatency{}
	}
	return f.latency()
}

var alertableJobs = []string{"orphaned audio reconcile", "deleted identity erasure", "acquisition prune"}

type fakeJobs struct {
	states map[string]*domain.JobState
}

func newFakeJobs() *fakeJobs {
	states := map[string]*domain.JobState{}
	for _, name := range alertableJobs {
		states[name] = &domain.JobState{}
	}
	return &fakeJobs{states: states}
}

func (f *fakeJobs) Alertable() []string { return alertableJobs }

func (f *fakeJobs) State(name string) domain.JobState { return *f.states[name] }

func healthy(context.Context) domain.DependencyHealth {
	return domain.DependencyHealth{DB: domain.DepUp, Redis: domain.DepUp, Auth: domain.DepUp}
}

var testNow = time.Date(2026, 11, 25, 10, 30, 0, 0, time.FixedZone("UTC+2", 2*3600))

func sources() service.AlertSources {
	return service.AlertSources{
		Clock:            func() time.Time { return testNow },
		DependencyHealth: healthy,
		Coverage:         &fakeCoverageEvents{},
		Sessions:         fakeSessions{},
		Jobs:             newFakeJobs(),
		Requests:         fakeRequests{},
	}
}

func mustConditions(s service.AlertSources) []domain.Condition {
	conditions, err := service.AlertConditions(s)
	if err != nil {
		panic(err)
	}
	return conditions
}

func conditionsByKey(s service.AlertSources) map[string]domain.Condition {
	byKey := map[string]domain.Condition{}
	for _, c := range mustConditions(s) {
		byKey[c.Key] = c
	}
	return byKey
}

func condition(t *testing.T, s service.AlertSources, key string) domain.Condition {
	t.Helper()
	c, ok := conditionsByKey(s)[key]
	if !ok {
		t.Fatalf("no %q condition", key)
	}
	return c
}

func coverageWatch(events *fakeCoverageEvents) (gap, queryFailing domain.Condition) {
	s := sources()
	s.Coverage = events
	byKey := conditionsByKey(s)
	return byKey["coverage_zero_result"], byKey["coverage_query_failing"]
}

func evalTick(ctx context.Context, conds ...domain.Condition) map[string]*domain.Alert {
	fired := make(map[string]*domain.Alert)
	for _, c := range conds {
		if a := c.Eval(ctx); a != nil {
			fired[c.Key] = a
		}
	}
	return fired
}

func TestConditions_WatchDependenciesCoverageCookiesAndEveryAlertableJob(t *testing.T) {
	conditions := mustConditions(sources())

	want := []string{"api_5xx_ratio", "dependency_down", "coverage_zero_result", "coverage_query_failing", "youtube_cookies_signed_out"}
	for _, name := range alertableJobs {
		want = append(want, "job_failing:"+name)
	}
	got := make([]string, 0, len(conditions))
	for _, c := range conditions {
		got = append(got, c.Key)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("condition keys = %v, want %v", got, want)
	}
}

func TestConditions_RefuseAMissingClockAtConstruction(t *testing.T) {
	s := sources()
	s.Clock = nil

	conditions, err := service.AlertConditions(s)

	if !errors.Is(err, service.ErrClockRequired) || conditions != nil {
		t.Fatalf("Conditions = (%v, %v), want no conditions and ErrClockRequired", conditions, err)
	}
}

func TestConditions_RefuseAMissingRequestSnapshotAtConstruction(t *testing.T) {
	s := sources()
	s.Requests = nil

	conditions, err := service.AlertConditions(s)

	if !errors.Is(err, service.ErrRequestsRequired) || conditions != nil {
		t.Fatalf("Conditions = (%v, %v), want no conditions and ErrRequestsRequired", conditions, err)
	}
}

func TestConditions_NothingFiresWhileAllIsWell(t *testing.T) {
	if fired := evalTick(context.Background(), mustConditions(sources())...); len(fired) != 0 {
		t.Fatalf("fired %v with every source healthy", fired)
	}
}

func TestCoverageCondition(t *testing.T) {
	ctx := context.Background()

	t.Run("fires on the true total even when the listed queries sum below the threshold", func(t *testing.T) {
		events := &fakeCoverageEvents{topN: []queryCount{{QueryNorm: "q", Count: 1}}, total: 25}
		gap, _ := coverageWatch(events)
		if alert := gap.Eval(ctx); alert == nil {
			t.Fatalf("alert did not fire: true total %d meets the threshold but the listed queries sum to 1", events.total)
		}
	})

	t.Run("24 zero-result searches do not fire", func(t *testing.T) {
		events := &fakeCoverageEvents{topN: []queryCount{{QueryNorm: "rare", Count: 3}}, total: 24}
		gap, _ := coverageWatch(events)
		if alert := gap.Eval(ctx); alert != nil {
			t.Fatalf("alert fired with total %d below the threshold of 25: %+v", events.total, alert)
		}
	})

	t.Run("25 zero-result searches fire", func(t *testing.T) {
		events := &fakeCoverageEvents{topN: []queryCount{{QueryNorm: "missing band", Count: 20}}, total: 25}
		gap, _ := coverageWatch(events)
		alert := gap.Eval(ctx)
		if alert == nil {
			t.Fatal("alert did not fire at total == 25")
		}
		if alert.Title != "altune discovery coverage gap" || alert.Severity != domain.SeverityWatch {
			t.Errorf("alert = %+v, want the coverage gap signal", alert)
		}
		if want := "zero-result searches in 24h: 25 (threshold 25); top query hit 20 times"; alert.Message != want {
			t.Errorf("message = %q, want %q", alert.Message, want)
		}
	})

	t.Run("no listed query leaves the top-query note out", func(t *testing.T) {
		gap, _ := coverageWatch(&fakeCoverageEvents{total: 30})
		alert := gap.Eval(ctx)
		if alert == nil || alert.Message != "zero-result searches in 24h: 30 (threshold 25)" {
			t.Fatalf("alert = %+v, want the bare count message", alert)
		}
	})
}

func TestCoverageCondition_CountsTheLast24Hours(t *testing.T) {
	events := &fakeCoverageEvents{total: 30}
	gap, _ := coverageWatch(events)

	gap.Eval(context.Background())

	if want := testNow.UTC().Add(-24 * time.Hour); !events.since.Equal(want) || events.since.Location() != time.UTC {
		t.Fatalf("zero-result total counted since %v, want exactly %v (the last 24h in UTC)", events.since, want)
	}
}

func TestCoverageCondition_MessageExcludesQueryText(t *testing.T) {
	const query = "my private search \"quoted\" term"
	gap, _ := coverageWatch(&fakeCoverageEvents{
		topN:  []queryCount{{QueryNorm: query, Count: 42}},
		total: 50,
	})
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

func TestCoverageConditions_QueryFailureIsNotHealthy(t *testing.T) {
	ctx := context.Background()

	t.Run("persistent failure escalates to its own signal", func(t *testing.T) {
		gap, queryFailing := coverageWatch(&fakeCoverageEvents{totalErr: errors.New("relation zero_result does not exist")})

		for i := 1; i < coverageQueryFailureEscalation; i++ {
			if fired := evalTick(ctx, gap, queryFailing); len(fired) != 0 {
				t.Fatalf("failure %d fired %v before escalation threshold %d", i, fired, coverageQueryFailureEscalation)
			}
		}
		alert := evalTick(ctx, gap, queryFailing)[queryFailing.Key]
		if alert == nil {
			t.Fatalf("%d consecutive query failures resolved to healthy; want a failure signal", coverageQueryFailureEscalation)
		}
		if alert.Severity != domain.SeverityWatch || alert.Title != "altune coverage alert check failing" {
			t.Errorf("alert = %+v, want the coverage-check-failing signal", alert)
		}
		if want := "coverage-gap query failed 3 consecutive times; gap status unknown"; alert.Message != want {
			t.Errorf("message = %q, want %q", alert.Message, want)
		}
	})

	t.Run("failure while a gap is firing does not resolve the gap", func(t *testing.T) {
		events := &fakeCoverageEvents{total: 50}
		gap, queryFailing := coverageWatch(events)
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
		gap, queryFailing := coverageWatch(events)
		for range coverageQueryFailureEscalation {
			evalTick(ctx, gap, queryFailing)
		}
		events.totalErr = nil
		events.total = 1
		if fired := evalTick(ctx, gap, queryFailing); len(fired) != 0 {
			t.Fatalf("fired %v after the query recovered below threshold", fired)
		}
		events.totalErr = errors.New("timeout again")
		if fired := evalTick(ctx, gap, queryFailing); len(fired) != 0 {
			t.Fatalf("one failure after a recovery fired %v, want the streak to start over", fired)
		}
	})
}

func TestDependencyCondition(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name    string
		health  domain.DependencyHealth
		wantMsg string
	}{
		{"auth only down names auth", domain.DependencyHealth{DB: domain.DepUp, Redis: domain.DepUp, Auth: domain.DepDown}, "dependencies down: auth"},
		{"db only down names db", domain.DependencyHealth{DB: domain.DepDown, Redis: domain.DepUp, Auth: domain.DepUp}, "dependencies down: db"},
		{"redis only down names redis", domain.DependencyHealth{DB: domain.DepUp, Redis: domain.DepDown, Auth: domain.DepUp}, "dependencies down: redis"},
		{"all down names all", domain.DependencyHealth{DB: domain.DepDown, Redis: domain.DepDown, Auth: domain.DepDown}, "dependencies down: db redis auth"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := sources()
			s.DependencyHealth = func(context.Context) domain.DependencyHealth { return tc.health }
			alert := condition(t, s, "dependency_down").Eval(ctx)
			if alert == nil {
				t.Fatal("alert = nil, want it to fire")
			}
			if alert.Message != tc.wantMsg || alert.Title != "altune dependency down" || alert.Severity != domain.SeverityWatch {
				t.Fatalf("alert = %+v, want the dependency signal with message %q", alert, tc.wantMsg)
			}
		})
	}

	t.Run("healthy does not fire", func(t *testing.T) {
		if alert := condition(t, sources(), "dependency_down").Eval(ctx); alert != nil {
			t.Fatalf("alert = %+v, want nil", alert)
		}
	})
}

func TestYouTubeCookiesCondition(t *testing.T) {
	since := time.Date(2026, 11, 20, 9, 30, 0, 0, time.FixedZone("EST", -5*3600))

	t.Run("a signed-out jar pages with the moment and the drop-in command", func(t *testing.T) {
		s := sources()
		s.Sessions = fakeSessions{since: since, signedOut: true}
		alert := condition(t, s, "youtube_cookies_signed_out").Eval(context.Background())
		if alert == nil {
			t.Fatal("alert = nil, want it to fire")
		}
		want := "YouTube has asked yt-dlp to sign in since 2026-11-20T14:30:00Z, so YouTube downloads fail. " +
			"Export cookies.txt from a browser signed in to YouTube, copy it to the VM and run: " +
			"cd ~/altune/services/go-api && bash deploy/cookie-jar-dir.sh <export>"
		if alert.Title != "altune YouTube cookies expired" || alert.Message != want || alert.Severity != domain.SeverityWatch {
			t.Errorf("alert = %+v, want the cookies-expired signal with message %q", alert, want)
		}
	})

	t.Run("a signed-in jar does not fire", func(t *testing.T) {
		if alert := condition(t, sources(), "youtube_cookies_signed_out").Eval(context.Background()); alert != nil {
			t.Fatalf("alert = %+v, want nil", alert)
		}
	})
}

func TestJobFailingCondition(t *testing.T) {
	ctx := context.Background()
	s := sources()
	runner := newFakeJobs()
	s.Jobs = runner
	state := runner.states["deleted identity erasure"]
	cond := condition(t, s, "job_failing:deleted identity erasure")

	state.ConsecutiveFailures = jobFailureEscalation - 1
	if got := cond.Eval(ctx); got != nil {
		t.Fatalf("fired below escalation: %+v", got)
	}

	state.ConsecutiveFailures = jobFailureEscalation
	got := cond.Eval(ctx)
	if got == nil {
		t.Fatal("consecutive failures produced no alert")
	}
	if want := `job "deleted identity erasure" failed 3 consecutive runs`; got.Message != want || got.Title != "altune background job failing" || got.Severity != domain.SeverityWatch {
		t.Fatalf("alert = %+v, want the job signal with message %q and no error text", got, want)
	}

	state.Disabled = true
	if got := cond.Eval(ctx); got != nil {
		t.Fatalf("disabled job fired: %+v", got)
	}
	state.Disabled = false

	state.ConsecutiveFailures = 0
	if got := cond.Eval(ctx); got != nil {
		t.Fatalf("recovered job still firing: %+v", got)
	}
}

func TestJobFailingCondition_UnreadableIdentityStoreFiresAfterEscalation(t *testing.T) {
	s := sources()
	runner := newFakeJobs()
	s.Jobs = runner
	cond := condition(t, s, "job_failing:deleted identity erasure")

	runner.states["deleted identity erasure"].ConsecutiveFailures = jobFailureEscalation

	if got := cond.Eval(context.Background()); got == nil {
		t.Fatal("unreadable identity store for consecutive runs produced no alert")
	}
}
