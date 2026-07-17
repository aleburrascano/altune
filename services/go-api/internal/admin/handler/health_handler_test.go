package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestDependencyHealth_Healthy(t *testing.T) {
	tests := []struct {
		name string
		dep  DependencyHealth
		want bool
	}{
		{"all up", DependencyHealth{DB: "ok", Redis: "ok"}, true},
		{"redis down", DependencyHealth{DB: "ok", Redis: "down"}, false},
		{"db down", DependencyHealth{DB: "down", Redis: "ok"}, false},
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

// AE1: the operator health tile reports the per-dependency breakdown (Redis
// down shows red, DB green) through the data endpoint.
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
