package goapi_test

import (
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

const metricsLiveBody = `{
	"auth": {"token_rejections_total": 3},
	"catalog": {"lookups_total": 10},
	"latency": {
		"routes": {
			"/v1/tracks/{trackId}": {
				"count": 100,
				"sum_ms": 750,
				"status": {"2xx": 90, "4xx": 6, "5xx": 4},
				"buckets": [
					{"le_ms": "1", "count": 0},
					{"le_ms": "5", "count": 0},
					{"le_ms": "10", "count": 100},
					{"le_ms": "+Inf", "count": 0}
				]
			}
		}
	}
}`

func TestAdminMetricsLiveDecodesStubbedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("stub got method %s, want GET", r.Method)
		}
		if r.URL.Path != "/observe/metrics/live" {
			t.Errorf("stub got path %s, want /observe/metrics/live", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(metricsLiveBody))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminMetricsLive(context.Background())
	if err != nil {
		t.Fatalf("AdminMetricsLive: unexpected error: %v", err)
	}
	route, ok := got.Latency.Routes["/v1/tracks/{trackId}"]
	if !ok {
		t.Fatalf("route not decoded; got routes %+v", got.Latency.Routes)
	}
	if route.Count != 100 || route.SumMs != 750 {
		t.Fatalf("route count/sum = %d/%d, want 100/750", route.Count, route.SumMs)
	}
	if len(route.Buckets) != 4 {
		t.Fatalf("decoded %d buckets, want 4", len(route.Buckets))
	}
	if route.Buckets[2].LeMs != "10" || route.Buckets[2].Count != 100 {
		t.Fatalf("bucket[2] = %+v, want le_ms=10 count=100", route.Buckets[2])
	}
	if route.Buckets[3].LeMs != "+Inf" {
		t.Fatalf("final bucket label = %q, want +Inf", route.Buckets[3].LeMs)
	}
}

func TestAdminMetricsLiveDecodesStatusClasses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(metricsLiveBody))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminMetricsLive(context.Background())
	if err != nil {
		t.Fatalf("AdminMetricsLive: unexpected error: %v", err)
	}

	status := got.Latency.Routes["/v1/tracks/{trackId}"].Status
	if status.Count2xx != 90 || status.Count4xx != 6 || status.Count5xx != 4 {
		t.Fatalf("status classes = %+v, want 2xx=90 4xx=6 5xx=4", status)
	}
}

func TestAdminMetricsLiveAttachesOperatorBearer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"latency":{"routes":{}}}`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).AdminMetricsLive(context.Background()); err != nil {
		t.Fatalf("AdminMetricsLive: %v", err)
	}
	if want := "Bearer " + testToken; gotAuth != want {
		t.Fatalf("Authorization = %q, want %q", gotAuth, want)
	}
}

func TestAdminMetricsLiveUnreachableYieldsSourceDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	_, err := newClient(t, url).AdminMetricsLive(context.Background())
	if !goapi.IsSourceDown(err) {
		t.Fatalf("error %v (%T) is not a SourceDownError", err, err)
	}
	var sd *goapi.SourceDownError
	if !errors.As(err, &sd) || sd.Err == nil {
		t.Fatalf("SourceDownError did not wrap the transport error: %+v", sd)
	}
}

func TestAdminMetricsLiveForbiddenYieldsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"operator access required"}`))
	}))
	defer srv.Close()

	_, err := newClient(t, srv.URL).AdminMetricsLive(context.Background())
	if goapi.IsSourceDown(err) {
		t.Fatalf("403 misclassified as source-down: %v", err)
	}
	var apiErr *goapi.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v (%T) is not an APIError", err, err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("APIError.StatusCode = %d, want 403", apiErr.StatusCode)
	}
}
