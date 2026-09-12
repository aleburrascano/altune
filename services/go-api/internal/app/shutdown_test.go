package app

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// TestShutdownComponent_TimeoutSurfacedDistinctly is the regression for #380:
// a component whose bounded shutdown exceeds its budget used to fall through to
// cleanup() identically to a clean completion, with no value returned and
// nothing logged — so the DB pool and Redis client were closed out from under
// still-running work with no signal. shutdownComponent must now report the
// timeout distinctly from a clean completion.
func TestShutdownComponent_TimeoutSurfacedDistinctly(t *testing.T) {
	a := &App{}

	started := make(chan struct{})
	slow := a.shutdownComponent("slow component", 30*time.Millisecond, func(ctx context.Context) {
		close(started)
		<-ctx.Done() // exceeds its budget; only the bounded ctx stops it
	})
	<-started
	if slow.completed {
		t.Fatal("component that exceeded its budget reported completed=true")
	}
	if slow.name != "slow component" {
		t.Errorf("outcome name: got %q, want %q", slow.name, "slow component")
	}

	fast := a.shutdownComponent("fast component", 5*time.Second, func(context.Context) {})
	if !fast.completed {
		t.Fatal("component that finished promptly reported completed=false")
	}

	// The two outcomes must be distinguishable, which is the whole point.
	if slow.completed == fast.completed {
		t.Fatal("timed-out and clean shutdowns are indistinguishable")
	}
}

func TestDrainBackground_TimeoutVsClean(t *testing.T) {
	clean := (&App{}).drainBackground(time.Second)
	if !clean.completed {
		t.Fatal("drain with no outstanding work reported completed=false")
	}

	a := &App{}
	a.wg.Add(1)
	defer a.wg.Done() // release the lingering task so the test goroutine exits cleanly

	timedOut := a.drainBackground(20 * time.Millisecond)
	if timedOut.completed {
		t.Fatal("drain that timed out with work still running reported completed=true")
	}
}

func TestUnfinishedShutdowns_NamesOnlyIncomplete(t *testing.T) {
	outcomes := []shutdownOutcome{
		{name: "alert monitor", completed: true},
		{name: "acquisition scheduler", completed: false},
		{name: "background tasks", completed: false},
	}

	got := unfinishedShutdowns(outcomes)
	want := []string{"acquisition scheduler", "background tasks"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unfinished: got %v, want %v", got, want)
	}
}

// TestShutdownComponent_LogsWarningNamingComponent confirms the timeout is not
// just returned but surfaced in the logs, naming the component that was still
// running when cleanup() would close the pool/Redis.
func TestShutdownComponent_LogsWarningNamingComponent(t *testing.T) {
	var buf bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(restore)

	a := &App{}
	a.shutdownComponent("acquisition scheduler", 10*time.Millisecond, func(ctx context.Context) {
		<-ctx.Done()
	})

	logged := buf.String()
	if !strings.Contains(logged, "exceeded its budget") {
		t.Errorf("expected a budget-exceeded warning, got: %q", logged)
	}
	if !strings.Contains(logged, "acquisition scheduler") {
		t.Errorf("warning did not name the stuck component, got: %q", logged)
	}
}
