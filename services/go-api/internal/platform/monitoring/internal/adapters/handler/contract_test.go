package handler_test

import (
	"altune/go-api/internal/platform/monitoring/internal/adapters/handler"
	"altune/go-api/internal/shared/clock"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func everyDependencyDown(context.Context) handler.DependencyHealth {
	return handler.DependencyHealth{
		DB:    handler.DepDown,
		Redis: handler.DepDown,
		Auth:  handler.DepDown,
		Detail: handler.DependencyDetail{
			DBError:    "db refused",
			RedisError: "redis refused",
			AuthError:  "jwks refused",
			CheckedAt:  time.Now(),
		},
	}
}

func serveObserveHealth(t *testing.T, probe func(context.Context) handler.DependencyHealth) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	handler.NewObserveHandler(clock.Now, clock.NewTicker, handler.Deps{Health: probe}).Register(r)
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

func TestNewObserveHandler_RefusesAMissingClock(t *testing.T) {
	defer func() {
		if msg, _ := recover().(string); !strings.Contains(msg, "NewObserveHandler") {
			t.Errorf("panic = %q, want a refusal naming NewObserveHandler", msg)
		}
	}()
	handler.NewObserveHandler(nil, clock.NewTicker, handler.Deps{})
}

func TestNewObserveHandler_RefusesAMissingTicker(t *testing.T) {
	defer func() {
		if msg, _ := recover().(string); !strings.Contains(msg, "nil ticker") {
			t.Errorf("panic = %q, want a refusal naming the ticker", msg)
		}
	}()
	handler.NewObserveHandler(clock.Now, nil, handler.Deps{})
}
