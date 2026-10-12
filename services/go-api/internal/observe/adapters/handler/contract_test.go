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

func everyDependencyDown(context.Context) DependencyHealth {
	return DependencyHealth{
		DB:    DepDown,
		Redis: DepDown,
		Auth:  DepDown,
		Detail: DependencyDetail{
			DBError:    "db refused",
			RedisError: "redis refused",
			AuthError:  "jwks refused",
			CheckedAt:  time.Now(),
		},
	}
}

func serveObserveHealth(t *testing.T, probe HealthProbe) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	NewObserveHandler(Deps{Health: probe}).Register(r)
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
