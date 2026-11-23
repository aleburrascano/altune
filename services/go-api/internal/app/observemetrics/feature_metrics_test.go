package observemetrics_test

import (
	"altune/go-api/internal/app/observemetrics"
	"altune/go-api/internal/observe"
	"altune/go-api/internal/shared/contracttest"
	"altune/go-api/internal/shared/reqmetrics"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestFeatureMetrics_NamesEveryFeaturesSnapshot(t *testing.T) {
	got := observemetrics.FeatureMetrics(reqmetrics.NewRegistry())

	for _, key := range []string{"auth", "catalog", "playback", "providers", "latency"} {
		if got[key] == nil {
			t.Errorf("feature metrics lack %q: %v", key, got)
		}
	}
	if len(got) != 5 {
		t.Errorf("feature metrics carry %d keys, want exactly the five features", len(got))
	}
}

func TestContract_MetricsLiveMatchesFixture(t *testing.T) {
	latency := reqmetrics.NewRegistry()
	latency.Observe("/v1/tracks/{trackId}", 5*time.Millisecond, 200)
	r := chi.NewRouter()
	observe.NewObserveHandler(observe.Deps{LiveMetrics: func() observe.LiveMetrics { return observemetrics.FeatureMetrics(latency) }}).Register(r)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics/live", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	contracttest.AssertMatchesFixture(t, filepath.Join("testdata", "contract", "observe_metrics_live.json"), rec.Body.Bytes())
}
