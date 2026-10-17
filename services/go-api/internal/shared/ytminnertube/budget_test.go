package ytminnertube

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func budgetOn(t *testing.T, server *miniredis.Miniredis, settings Settings) *Budget {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	budget, err := NewBudget(client, settings)
	if err != nil {
		t.Fatal(err)
	}
	return budget
}

func TestBudget_TwoCallersShareOneBucket(t *testing.T) {
	server := miniredis.RunT(t)
	interval := 300 * time.Millisecond
	app := budgetOn(t, server, Settings{MinInterval: interval})
	tool := budgetOn(t, server, Settings{MinInterval: interval})

	start := time.Now()
	for _, caller := range []*Budget{app, tool, app, tool} {
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
	budget := budgetOn(t, miniredis.RunT(t), Settings{Cooldown: cooldown})
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
	budget := budgetOn(t, miniredis.RunT(t), Settings{MinInterval: time.Minute})
	if err := budget.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := budget.Wait(ctx); !errors.Is(err, ErrBudgetRefused) {
		t.Fatalf("got %v, want ErrBudgetRefused", err)
	}
}

func TestBudgetTransport_FailsClosedWhenRedisIsDown(t *testing.T) {
	server := miniredis.RunT(t)
	budget := budgetOn(t, server, Settings{})
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

func TestSettingsFromEnv(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		want    Settings
		wantErr bool
	}{
		{"defaults", nil, Settings{MinInterval: DefaultMinInterval, Cooldown: DefaultCooldown}, false},
		{"both set", map[string]string{minIntervalVarName: "5s", cooldownVarName: "1m"}, Settings{MinInterval: 5 * time.Second, Cooldown: time.Minute}, false},
		{"bad interval", map[string]string{minIntervalVarName: "fast"}, Settings{}, true},
		{"negative cooldown", map[string]string{cooldownVarName: "-1s"}, Settings{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SettingsFromEnv(func(k string) string { return tc.env[k] })
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestToolClient_RefusesToRunWithoutRedis(t *testing.T) {
	_, _, err := ToolClient(context.Background(), func(string) string { return "" })
	if !errors.Is(err, ErrNoRedisForTool) {
		t.Fatalf("got %v, want ErrNoRedisForTool", err)
	}
}

func TestToolClient_PacesThroughTheSharedBucket(t *testing.T) {
	server := miniredis.RunT(t)
	env := func(key string) string {
		return map[string]string{redisURLVarName: "redis://" + server.Addr(), minIntervalVarName: "300ms"}[key]
	}
	client, settings, err := ToolClient(context.Background(), env)
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
	if _, err := NewBudget(nil, Settings{}); err == nil {
		t.Fatal("expected an error for a nil client")
	}
}

func TestSettings_DescribeNamesTheSharedPaceAndCooldown(t *testing.T) {
	got := Settings{MinInterval: 2 * time.Second, Cooldown: 10 * time.Minute}.Describe()
	want := "shared InnerTube budget: one call per 2s across the app and the tools, 10m0s cooldown after a 403"
	if got != want {
		t.Fatalf("Describe() = %q, want %q", got, want)
	}
}

func TestBudget_RefusesASlotThatLeavesNoTimeForTheRequestWithoutReservingIt(t *testing.T) {
	server := miniredis.RunT(t)
	budget := budgetOn(t, server, Settings{MinInterval: 9900 * time.Millisecond})
	if err := budget.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	slotBefore, err := server.Get(budgetKey)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := budget.Wait(ctx); !errors.Is(err, ErrBudgetRefused) {
		t.Fatalf("slot 9.9s out under a 10s deadline: got %v, want ErrBudgetRefused", err)
	}
	if slotAfter, _ := server.Get(budgetKey); slotAfter != slotBefore {
		t.Fatalf("refused call moved the next slot from %s to %s, want it left alone", slotBefore, slotAfter)
	}
}
