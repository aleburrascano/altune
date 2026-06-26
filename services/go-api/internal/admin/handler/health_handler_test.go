package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
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
// down shows red, DB green) through the gated endpoint.
func TestAdminHealthEndpoint(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	probe := func(context.Context) DependencyHealth {
		return DependencyHealth{DB: "ok", Redis: "down"}
	}
	h := New(operator.String(), probe, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req = req.WithContext(auth.ContextWithUserID(req.Context(), operator))
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)

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
