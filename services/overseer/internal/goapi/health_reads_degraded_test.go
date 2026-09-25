package goapi_test

import (
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminHealthDegradedBodyDecodesLive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"db":"ok","redis":"down","auth":"ok","detail":{"redis_error":"dial tcp: connection refused"}}`))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminHealth(context.Background())
	if err != nil {
		t.Fatalf("AdminHealth: unexpected error on 503 body: %v", err)
	}
	if got.Healthy() {
		t.Fatal("Healthy() = true despite redis down")
	}
	if got.DB != "ok" || got.Redis != "down" || got.Auth != "ok" {
		t.Fatalf("dependency pills = %+v, want db=ok redis=down auth=ok", got)
	}
	if got.Detail.RedisError == "" {
		t.Fatal("RedisError empty; the 503 detail did not decode")
	}
}

func TestAdminHealthNon503NonOKStillAPIError(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusInternalServerError} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"db":"ok","redis":"down","auth":"ok"}`))
		}))

		_, err := newClient(t, srv.URL).AdminHealth(context.Background())
		srv.Close()

		var apiErr *goapi.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("status %d: error %v (%T) is not an APIError", status, err, err)
		}
		if apiErr.StatusCode != status {
			t.Fatalf("APIError.StatusCode = %d, want %d", apiErr.StatusCode, status)
		}
		if goapi.Classify(err) == goapi.ReasonDegraded {
			t.Fatalf("status %d misclassified as degraded", status)
		}
	}
}
