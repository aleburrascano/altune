package handler_test

import (
	"altune/go-api/internal/platform/monitoring"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

const probeBudget = 5 * time.Second

func TestHealth_StatusFollowsTheDependencies(t *testing.T) {
	tests := []struct {
		name string
		dep  monitoring.DependencyHealth
		want int
	}{
		{"all up", monitoring.DependencyHealth{DB: monitoring.DepUp, Redis: monitoring.DepUp, Auth: monitoring.DepUp}, http.StatusOK},
		{"db down", monitoring.DependencyHealth{DB: monitoring.DepDown, Redis: monitoring.DepUp, Auth: monitoring.DepUp}, http.StatusServiceUnavailable},
		{"redis down", monitoring.DependencyHealth{DB: monitoring.DepUp, Redis: monitoring.DepDown, Auth: monitoring.DepUp}, http.StatusServiceUnavailable},
		{"auth down", monitoring.DependencyHealth{DB: monitoring.DepUp, Redis: monitoring.DepUp, Auth: monitoring.DepDown}, http.StatusServiceUnavailable},
		{"unconfigured is still ready", monitoring.DependencyHealth{DB: monitoring.DepUp, Redis: monitoring.DepNotConfigured, Auth: monitoring.DepNotConfigured}, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := func(context.Context) monitoring.DependencyHealth { return tt.dep }
			if got := serveObserveHealth(t, probe).Code; got != tt.want {
				t.Errorf("status = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestHealth_ProbeRunsUnderADeadline(t *testing.T) {
	var deadline time.Time
	var hasDeadline bool
	probe := func(ctx context.Context) monitoring.DependencyHealth {
		deadline, hasDeadline = ctx.Deadline()
		return monitoring.DependencyHealth{DB: monitoring.DepUp, Redis: monitoring.DepUp, Auth: monitoring.DepUp}
	}
	serveObserveHealth(t, probe)

	if !hasDeadline {
		t.Fatal("probe ran with no deadline, so a stalled dependency would park the request")
	}
	if remaining := time.Until(deadline); remaining > probeBudget {
		t.Errorf("probe deadline %v away, want at most %v", remaining, probeBudget)
	}
}

func TestHealth_StalledProbeIsReleasedWhenTheRequestEnds(t *testing.T) {
	r := chi.NewRouter()
	monitoring.NewObserveHandler(monitoring.Deps{Health: func(ctx context.Context) monitoring.DependencyHealth {
		<-ctx.Done()
		return monitoring.DependencyHealth{DB: monitoring.DepDown}
	}}).Register(r)
	ctx, endRequest := context.WithCancel(context.Background())
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/health", nil))
	}()
	endRequest()

	select {
	case <-done:
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503 from the stalled probe's down report", rec.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("a stalled probe was never cancelled when its request ended")
	}
}

func TestHealth_ProbeBudgetIsFiveSeconds(t *testing.T) {
	var deadline time.Time
	probe := func(ctx context.Context) monitoring.DependencyHealth {
		deadline, _ = ctx.Deadline()
		return monitoring.DependencyHealth{}
	}
	serveObserveHealth(t, probe)

	if remaining := time.Until(deadline); remaining <= probeBudget-time.Second || remaining > probeBudget {
		t.Errorf("probe deadline %v away, want just under %v", remaining, probeBudget)
	}
}

func TestHealth_HeapIsReportedInMebibytes(t *testing.T) {
	const mebibyte = 1024 * 1024
	ballast := make([]byte, 64*mebibyte)
	ballast[len(ballast)-1] = 1

	body := serveObserveHealth(t, func(context.Context) monitoring.DependencyHealth { return monitoring.DependencyHealth{} }).Body.Bytes()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(ballast)
	var got struct {
		HeapMB uint64 `json:"heap_mb"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.HeapMB < 64 || got.HeapMB > after.HeapAlloc/mebibyte+64 {
		t.Errorf("heap_mb = %d with a 64 MiB ballast live (heap now %d MiB)", got.HeapMB, after.HeapAlloc/mebibyte)
	}
}
