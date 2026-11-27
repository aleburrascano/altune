package youtubemusicapi_test

import (
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/youtubemusicapi"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

const (
	budgetRedisKey = "ytminnertube:budget"
	minIntervalVar = "YTM_INNERTUBE_MIN_INTERVAL"
	cooldownVar    = "YTM_INNERTUBE_COOLDOWN"
	redisURLVar    = "REDIS_URL"
)

type budgetCaller interface {
	Wait(ctx context.Context) error
	CoolDown(ctx context.Context) error
	Transport(base http.RoundTripper) http.RoundTripper
}

func budgetOn(t *testing.T, server *miniredis.Miniredis, settings youtubemusicapi.Settings) budgetCaller {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	budget, err := youtubemusicapi.NewBudget(client, settings, clock.Now, clock.Sleep)
	if err != nil {
		t.Fatal(err)
	}
	return budget
}

func TestBudget_TwoCallersShareOneBucket(t *testing.T) {
	server := miniredis.RunT(t)
	interval := 300 * time.Millisecond
	app := budgetOn(t, server, youtubemusicapi.Settings{MinInterval: interval})
	tool := budgetOn(t, server, youtubemusicapi.Settings{MinInterval: interval})

	start := time.Now()
	for _, caller := range []budgetCaller{app, tool, app, tool} {
		if err := caller.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 3*interval-50*time.Millisecond {
		t.Fatalf("four calls from two callers took %v, want at least %v", elapsed, 3*interval)
	}
}

func TestBudgetTransport_A403MakesTheNextCallWaitForTheCooldown(t *testing.T) {
	forbidden := true
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if forbidden {
			forbidden = false
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	t.Cleanup(upstream.Close)
	cooldown := 400 * time.Millisecond
	budget := budgetOn(t, miniredis.RunT(t), youtubemusicapi.Settings{Cooldown: cooldown})
	client := &http.Client{Transport: budget.Transport(nil)}

	get := func() int {
		resp, err := client.Get(upstream.URL)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if status := get(); status != http.StatusForbidden {
		t.Fatalf("first status %d, want 403", status)
	}
	start := time.Now()
	get()
	if elapsed := time.Since(start); elapsed < cooldown-50*time.Millisecond {
		t.Fatalf("next call went out after %v, want at least %v", elapsed, cooldown)
	}
}

func TestBudget_RefusesACallThatCannotBeServedBeforeItsDeadline(t *testing.T) {
	budget := budgetOn(t, miniredis.RunT(t), youtubemusicapi.Settings{MinInterval: time.Minute})
	if err := budget.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := budget.Wait(ctx); !errors.Is(err, youtubemusicapi.ErrBudgetRefused) {
		t.Fatalf("got %v, want ErrBudgetRefused", err)
	}
}

func TestBudgetTransport_FailsClosedWhenRedisIsDown(t *testing.T) {
	server := miniredis.RunT(t)
	budget := budgetOn(t, server, youtubemusicapi.Settings{})
	server.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the upstream was called without a budget")
	}))
	t.Cleanup(upstream.Close)
	client := &http.Client{Transport: budget.Transport(nil)}
	if resp, err := client.Get(upstream.URL); err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected an error with redis down")
	}
}

func TestToolClient_SettingsFromEnv(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		want    youtubemusicapi.Settings
		wantErr bool
	}{
		{"defaults", nil, youtubemusicapi.Settings{MinInterval: 2 * time.Second, Cooldown: 10 * time.Minute}, false},
		{"both set", map[string]string{minIntervalVar: "5s", cooldownVar: "1m"}, youtubemusicapi.Settings{MinInterval: 5 * time.Second, Cooldown: time.Minute}, false},
		{"bad interval", map[string]string{minIntervalVar: "fast"}, youtubemusicapi.Settings{}, true},
		{"negative cooldown", map[string]string{cooldownVar: "-1s"}, youtubemusicapi.Settings{}, true},
	}
	server := miniredis.RunT(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := func(k string) string {
				if k == redisURLVar {
					return "redis://" + server.Addr()
				}
				return tc.env[k]
			}
			_, got, err := youtubemusicapi.ToolClient(context.Background(), env, time.Second)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestToolClient_RefusesToRunWithoutRedis(t *testing.T) {
	_, _, err := youtubemusicapi.ToolClient(context.Background(), func(string) string { return "" }, time.Second)
	if err == nil || !strings.Contains(err.Error(), redisURLVar+" is not set") {
		t.Fatalf("got %v, want the missing REDIS_URL refusal", err)
	}
}

func TestToolClient_PacesThroughTheSharedBucket(t *testing.T) {
	server := miniredis.RunT(t)
	env := func(key string) string {
		return map[string]string{redisURLVar: "redis://" + server.Addr(), minIntervalVar: "300ms"}[key]
	}
	client, settings, err := youtubemusicapi.ToolClient(context.Background(), env, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(upstream.Close)
	app := budgetOn(t, server, settings)
	if err := app.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil {
		t.Fatal("nil response without an error")
	}
	_ = resp.Body.Close()
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Fatalf("tool call went out %v after the app's, want the shared 300ms spacing", elapsed)
	}
}

func TestNewBudget_NeedsARedisClient(t *testing.T) {
	if _, err := youtubemusicapi.NewBudget(nil, youtubemusicapi.Settings{}, clock.Now, clock.Sleep); err == nil {
		t.Fatal("expected an error for a nil client")
	}
}

func TestNewBudget_RefusesANilClock(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = client.Close() })

	budget, err := youtubemusicapi.NewBudget(client, youtubemusicapi.Settings{}, nil, clock.Sleep)

	if err == nil || !strings.Contains(err.Error(), "requires a clock") || budget != nil || !strings.Contains(err.Error(), "NewBudget") {
		t.Fatalf("NewBudget(nil clock) = (%v, %v), want ErrClockRequired naming the constructor and no budget", budget, err)
	}
}

func TestNewBudget_RefusesANilSleeper(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = client.Close() })

	budget, err := youtubemusicapi.NewBudget(client, youtubemusicapi.Settings{}, clock.Now, nil)

	if err == nil || !strings.Contains(err.Error(), "requires a sleeper") || budget != nil {
		t.Fatalf("NewBudget(nil sleeper) = (%v, %v), want ErrSleeperRequired and no budget", budget, err)
	}
	if !strings.Contains(err.Error(), "NewBudget") {
		t.Fatalf("error %q does not name the constructor", err)
	}
}

func TestBudget_WaitsThroughTheSleeperItWasGiven(t *testing.T) {
	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	var asked []time.Duration
	budget, err := youtubemusicapi.NewBudget(client, youtubemusicapi.Settings{MinInterval: time.Hour}, clock.Now, func(_ context.Context, d time.Duration) error {
		asked = append(asked, d)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := budget.Wait(t.Context()); err != nil {
			t.Fatalf("Wait: %v", err)
		}
	}

	if len(asked) != 2 || asked[0] != 0 || asked[1] <= 0 {
		t.Fatalf("the sleeper was asked for %v, want a zero wait then a positive one", asked)
	}
}

func TestSettings_DescribeNamesTheSharedPaceAndCooldown(t *testing.T) {
	got := youtubemusicapi.Settings{MinInterval: 2 * time.Second, Cooldown: 10 * time.Minute}.Describe()
	want := "shared InnerTube budget: one call per 2s across the app and the tools, 10m0s cooldown after a 403"
	if got != want {
		t.Fatalf("Describe() = %q, want %q", got, want)
	}
}

func TestBudget_RefusesASlotThatLeavesNoTimeForTheRequestWithoutReservingIt(t *testing.T) {
	server := miniredis.RunT(t)
	budget := budgetOn(t, server, youtubemusicapi.Settings{MinInterval: 9900 * time.Millisecond})
	if err := budget.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	slotBefore, err := server.Get(budgetRedisKey)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := budget.Wait(ctx); !errors.Is(err, youtubemusicapi.ErrBudgetRefused) {
		t.Fatalf("slot 9.9s out under a 10s deadline: got %v, want ErrBudgetRefused", err)
	}
	if slotAfter, _ := server.Get(budgetRedisKey); slotAfter != slotBefore {
		t.Fatalf("refused call moved the next slot from %s to %s, want it left alone", slotBefore, slotAfter)
	}
}

func TestBudgetTransport_TakesOneSlotWhenTheCallerReservedFirst(t *testing.T) {
	const interval = 200 * time.Millisecond
	cases := []struct {
		name    string
		reserve bool
	}{
		{name: "caller reserved first", reserve: true},
		{name: "caller did not reserve", reserve: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := miniredis.RunT(t)
			budget := budgetOn(t, server, youtubemusicapi.Settings{MinInterval: interval})
			upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			t.Cleanup(upstream.Close)
			client := &http.Client{Transport: budget.Transport(nil)}
			if err := budget.Wait(context.Background()); err != nil {
				t.Fatal(err)
			}
			slotBefore := slotMillis(t, server)

			ctx := context.Background()
			if tc.reserve {
				reserved, err := youtubemusicapi.ReserveSlot(ctx, client)
				if err != nil {
					t.Fatal(err)
				}
				ctx = reserved
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL, http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()

			if taken := slotMillis(t, server) - slotBefore; taken != interval.Milliseconds() {
				t.Fatalf("the call advanced the next slot by %dms, want exactly one interval of %v", taken, interval)
			}
		})
	}
}

func slotMillis(t *testing.T, server *miniredis.Miniredis) int64 {
	t.Helper()
	raw, err := server.Get(budgetRedisKey)
	if err != nil {
		t.Fatal(err)
	}
	slot, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return slot
}

func TestBudget_CoolDownSucceedsOnALiveRedisAndNamesTheScriptWhenRedisIsDown(t *testing.T) {
	server := miniredis.RunT(t)
	budget := budgetOn(t, server, youtubemusicapi.Settings{Cooldown: time.Minute})

	if err := budget.CoolDown(context.Background()); err != nil {
		t.Fatalf("CoolDown on a live redis = %v, want nil", err)
	}
	server.Close()
	err := budget.CoolDown(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cooldown script") {
		t.Fatalf("CoolDown with redis down = %v, want an error naming the cooldown script", err)
	}
}

func TestToolClient_RefusesANonPositiveTimeout(t *testing.T) {
	server := miniredis.RunT(t)
	env := func(key string) string { return map[string]string{redisURLVar: "redis://" + server.Addr()}[key] }

	_, _, err := youtubemusicapi.ToolClient(context.Background(), env, 0)

	if !errors.Is(err, outbound.ErrClientTimeoutRequired) {
		t.Fatalf("got %v, want ErrClientTimeoutRequired", err)
	}
}
