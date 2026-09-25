package handler

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestHealth_StatusFollowsTheDependencies(t *testing.T) {
	tests := []struct {
		name string
		dep  DependencyHealth
		want int
	}{
		{"all up", DependencyHealth{DB: DepUp, Redis: DepUp, Auth: DepUp}, http.StatusOK},
		{"db down", DependencyHealth{DB: DepDown, Redis: DepUp, Auth: DepUp}, http.StatusServiceUnavailable},
		{"redis down", DependencyHealth{DB: DepUp, Redis: DepDown, Auth: DepUp}, http.StatusServiceUnavailable},
		{"auth down", DependencyHealth{DB: DepUp, Redis: DepUp, Auth: DepDown}, http.StatusServiceUnavailable},
		{"unconfigured is still ready", DependencyHealth{DB: DepUp, Redis: DepNotConfigured, Auth: DepNotConfigured}, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := func(context.Context) DependencyHealth { return tt.dep }
			if got := serveObserveHealth(t, probe).Code; got != tt.want {
				t.Errorf("status = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestHealth_ProbeRunsUnderADeadline(t *testing.T) {
	var deadline time.Time
	var hasDeadline bool
	probe := func(ctx context.Context) DependencyHealth {
		deadline, hasDeadline = ctx.Deadline()
		return DependencyHealth{DB: DepUp, Redis: DepUp, Auth: DepUp}
	}
	serveObserveHealth(t, probe)

	if !hasDeadline {
		t.Fatal("probe ran with no deadline, so a stalled dependency would park the request")
	}
	if remaining := time.Until(deadline); remaining > defaultProbeTimeout {
		t.Errorf("probe deadline %v away, want at most %v", remaining, defaultProbeTimeout)
	}
}

func TestHealth_StalledProbeSeesItsDeadline(t *testing.T) {
	h := New(Deps{Health: func(ctx context.Context) DependencyHealth {
		<-ctx.Done()
		return DependencyHealth{DB: DepDown}
	}})
	h.probeTimeout = 10 * time.Millisecond

	done := make(chan DependencyHealth, 1)
	go func() { done <- h.runProbe(context.Background()) }()
	select {
	case got := <-done:
		if got.DB != DepDown {
			t.Errorf("DB = %q, want down", got.DB)
		}
	case <-time.After(time.Second):
		t.Fatal("a stalled probe was never cancelled")
	}
}
