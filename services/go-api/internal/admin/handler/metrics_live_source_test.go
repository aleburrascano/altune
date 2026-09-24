package handler_test

import (
	"altune/go-api/internal/admin/handler"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func serveLiveMetrics(h *handler.AdminHandler) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	h.RegisterData(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics/live", nil))
	return rec
}

func TestMetricsLiveSource_ServesInjectedValue(t *testing.T) {
	t.Parallel()
	h := handler.New(nil, nil).WithLiveMetrics(func() any {
		return map[string]int{"stub": 7}
	})

	rec := serveLiveMetrics(h)

	if rec.Code != http.StatusOK || rec.Body.String() != "{\"stub\":7}\n" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

func TestMetricsLiveSource_UnwiredIsEmptyObject(t *testing.T) {
	t.Parallel()

	rec := serveLiveMetrics(handler.New(nil, nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "{}\n" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}
