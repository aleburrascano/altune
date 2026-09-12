package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestDependencyHealth_Healthy(t *testing.T) {
	tests := []struct {
		name string
		dep  DependencyHealth
		want bool
	}{
		{"all up", DependencyHealth{DB: "ok", Redis: "ok", Auth: "ok"}, true},
		{"redis down", DependencyHealth{DB: "ok", Redis: "down"}, false},
		{"db down", DependencyHealth{DB: "down", Redis: "ok"}, false},
		{"auth down", DependencyHealth{DB: "ok", Redis: "ok", Auth: "down"}, false},
		{"auth not configured is still ready", DependencyHealth{DB: "ok", Redis: "ok", Auth: "not_configured"}, true},
		{"redis not configured is still ready", DependencyHealth{DB: "ok", Redis: "not_configured"}, true},
		{"both not configured is ready", DependencyHealth{DB: "not_configured", Redis: "not_configured"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.dep.Healthy(); got != tt.want {
				t.Errorf("Healthy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAdminHealthEndpoint(t *testing.T) {
	probe := func(context.Context) DependencyHealth {
		return DependencyHealth{DB: "ok", Redis: "down"}
	}
	h := New(probe, nil)

	r := chi.NewRouter()
	h.RegisterData(r)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got DependencyHealth
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.DB != "ok" || got.Redis != "down" {
		t.Errorf("tile data = %+v, want db ok / redis down", got)
	}
}

// TestAdminHealth_StuckProbeDoesNotHangRequest checks that a probe which only
// returns once its context is cancelled (a stalled DB/Redis) cannot park an
// /admin/health request forever: the bounded timeout lets it return.
func TestAdminHealth_StuckProbeDoesNotHangRequest(t *testing.T) {
	probe := func(ctx context.Context) DependencyHealth {
		<-ctx.Done()
		return DependencyHealth{DB: "down", Redis: "down"}
	}
	h := New(probe, nil)
	h.probeTimeout = 20 * time.Millisecond

	r := chi.NewRouter()
	h.RegisterData(r)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		r.ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("health request hung on a stuck probe")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
