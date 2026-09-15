package app

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// TestRunShutdownSequence_OrderPinned pins the shutdown order: the background
// drain must precede the leader-election release (so the next leader cannot
// start duplicate jobs), and the search drain runs last before cleanup().
func TestRunShutdownSequence_OrderPinned(t *testing.T) {
	want := []string{
		"alert monitor",
		"event feed",
		"eval meter",
		"vocabulary refresh",
		"acquisition scheduler",
		"background tasks",
		"leader election",
		"discovery search",
	}

	outcomes := (&App{}).runShutdownSequence()

	got := make([]string, 0, len(outcomes))
	for _, o := range outcomes {
		if !o.completed {
			t.Errorf("component %q did not complete on an empty App", o.name)
		}
		got = append(got, o.name)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("shutdown order:\n got  %v\n want %v", got, want)
	}
}

// TestShutdownPlan_TimeoutsPinned pins each component's shutdown budget.
func TestShutdownPlan_TimeoutsPinned(t *testing.T) {
	want := map[string]time.Duration{
		"alert monitor":         5 * time.Second,
		"event feed":            5 * time.Second,
		"eval meter":            5 * time.Second,
		"vocabulary refresh":    10 * time.Second,
		"acquisition scheduler": 30 * time.Second,
		"background tasks":      30 * time.Second,
		"leader election":       5 * time.Second,
		"discovery search":      30 * time.Second,
	}

	plan := (&App{}).shutdownPlan()
	if len(plan) != len(want) {
		t.Fatalf("plan has %d components, want %d", len(plan), len(want))
	}
	for _, c := range plan {
		if c.timeout != want[c.name] {
			t.Errorf("%s timeout: got %v, want %v", c.name, c.timeout, want[c.name])
		}
	}
}

// TestDrains_LogSharedBudgetWarning confirms both drains now emit
// shutdownComponent's warning shape, naming the stuck component.
func TestDrains_LogSharedBudgetWarning(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(restore)

	a := &App{}
	a.wg.Add(1)
	defer a.wg.Done()
	a.drainBackground(10 * time.Millisecond)

	logged := buf.String()
	if !strings.Contains(logged, "component shutdown exceeded its budget") ||
		!strings.Contains(logged, `component="background tasks"`) {
		t.Errorf("drain warning does not match shutdownComponent's shape: %q", logged)
	}
}
