package handler_test

import (
	"altune/go-api/internal/observe"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func everyDependencyDown(context.Context) observe.DependencyHealth {
	return observe.DependencyHealth{
		DB:    observe.DepDown,
		Redis: observe.DepDown,
		Auth:  observe.DepDown,
		Detail: observe.DependencyDetail{
			DBError:    "db refused",
			RedisError: "redis refused",
			AuthError:  "jwks refused",
			CheckedAt:  time.Now(),
		},
	}
}

func serveObserveHealth(t *testing.T, probe func(context.Context) observe.DependencyHealth) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	observe.NewObserveHandler(observe.Deps{Health: probe}).Register(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	return rec
}

func decodeObject(t *testing.T, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return object
}

func TestContract_HealthMatchesFixture(t *testing.T) {
	rec := serveObserveHealth(t, everyDependencyDown)
	assertMatchesFixture(t, "observe_health.json", rec.Body.Bytes())
}
