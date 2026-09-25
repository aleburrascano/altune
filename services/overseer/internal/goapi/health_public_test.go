package goapi_test

import (
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthProbesPublicEvenWhenTokenSourceAlwaysErrors(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("Authorization = %q, want none on the public probe", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c, err := goapi.New(srv.URL, goapi.StaticTokenSource(""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health with an always-erroring token source: %v", err)
	}
	if !hit {
		t.Fatal("Health never reached the server despite the endpoint being public")
	}
	if !got.OK() {
		t.Fatalf("Health = %+v, want ok", got)
	}
}

func TestHealthDegradedBodyIsReadableNotAnAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"degraded"}`))
	}))
	defer srv.Close()

	c := newClient(t, srv.URL)
	got, err := c.Health(context.Background())
	if err != nil {
		var apiErr *goapi.APIError
		if errors.As(err, &apiErr) {
			t.Fatalf("Health: %v, want the 503 degraded body decoded, not an APIError", err)
		}
		t.Fatalf("Health: unexpected error: %v", err)
	}
	if got.OK() {
		t.Fatalf("Health = %+v, want not ok", got)
	}
	if !got.Degraded() {
		t.Fatalf("Health = %+v, want degraded", got)
	}
}
