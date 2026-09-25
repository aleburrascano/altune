package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type overseerOperatorHealth struct {
	DB         string               `json:"db"`
	Redis      string               `json:"redis"`
	Auth       string               `json:"auth"`
	Detail     overseerHealthDetail `json:"detail"`
	Goroutines int                  `json:"goroutines"`
	HeapMB     uint64               `json:"heap_mb"`
}

type overseerHealthDetail struct {
	DBLatencyMs    int64     `json:"db_latency_ms"`
	DBError        string    `json:"db_error,omitempty"`
	RedisLatencyMs int64     `json:"redis_latency_ms"`
	RedisError     string    `json:"redis_error,omitempty"`
	AuthLatencyMs  int64     `json:"auth_latency_ms"`
	AuthError      string    `json:"auth_error,omitempty"`
	CheckedAt      time.Time `json:"checked_at"`
}

func jsonKeys(t reflect.Type) []string {
	keys := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		keys = append(keys, strings.Split(t.Field(i).Tag.Get("json"), ",")[0])
	}
	return keys
}

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
	New(Deps{Health: probe}).Register(r)
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

func TestContract_HealthBodyCarriesEveryKeyOverseerDecodes(t *testing.T) {
	body := decodeObject(t, serveObserveHealth(t, everyDependencyDown).Body.Bytes())

	for _, key := range jsonKeys(reflect.TypeOf(overseerOperatorHealth{})) {
		if _, present := body[key]; !present {
			t.Errorf("/observe/health body lacks %q, which Overseer's OperatorHealth decodes", key)
		}
	}
	detail := decodeObject(t, body["detail"])
	for _, key := range jsonKeys(reflect.TypeOf(overseerHealthDetail{})) {
		if _, present := detail[key]; !present {
			t.Errorf("/observe/health detail lacks %q, which Overseer's HealthDetail decodes", key)
		}
	}
}

func TestContract_HealthBodyDecodesIntoOverseerMirror(t *testing.T) {
	var got overseerOperatorHealth
	if err := json.Unmarshal(serveObserveHealth(t, everyDependencyDown).Body.Bytes(), &got); err != nil {
		t.Fatalf("decode into Overseer's mirror: %v", err)
	}
	if got.DB != "down" || got.Redis != "down" || got.Auth != "down" {
		t.Errorf("statuses = %q/%q/%q, want down/down/down", got.DB, got.Redis, got.Auth)
	}
	if got.Detail.DBError != "db refused" || got.Detail.CheckedAt.IsZero() {
		t.Errorf("detail = %+v, want the probe's error and check time", got.Detail)
	}
	if got.Goroutines <= 0 {
		t.Errorf("goroutines = %d, want the live count", got.Goroutines)
	}
}
