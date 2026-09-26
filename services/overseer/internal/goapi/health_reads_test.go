package goapi_test

import (
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const adminHealthBody = `{
	"db": "ok",
	"redis": "ok",
	"auth": "ok",
	"detail": {
		"db_latency_ms": 3,
		"redis_latency_ms": 1,
		"auth_latency_ms": 12,
		"checked_at": "2026-09-15T10:04:05Z"
	},
	"goroutines": 42,
	"heap_mb": 17
}`

// TestAdminHealthDecodesStubbedResponse is the core Done proof: the client hits a
// stubbed go-api /admin/health and gets a fully decoded operator health snapshot.
func TestAdminHealthDecodesStubbedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("stub got method %s, want GET", r.Method)
		}
		if r.URL.Path != "/observe/health" {
			t.Errorf("stub got path %s, want /observe/health", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(adminHealthBody))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminHealth(context.Background())
	if err != nil {
		t.Fatalf("AdminHealth: unexpected error: %v", err)
	}
	if got.DB != "ok" || got.Redis != "ok" || got.Auth != "ok" {
		t.Fatalf("dependency pills = %+v, want all ok", got)
	}
	if got.Detail.AuthLatencyMs != 12 || got.Goroutines != 42 || got.HeapMB != 17 {
		t.Fatalf("decoded detail/gauges = %+v, want auth=12 goroutines=42 heap=17", got)
	}
	want := time.Date(2026, 9, 15, 10, 4, 5, 0, time.UTC)
	if !got.Detail.CheckedAt.Equal(want) {
		t.Fatalf("CheckedAt = %v, want %v", got.Detail.CheckedAt, want)
	}
	if !got.Healthy() {
		t.Fatal("Healthy() = false for an all-ok snapshot")
	}
}

// TestAdminHealthReportsDownDependency proves a degraded snapshot decodes and the
// down verdict propagates: a single down pill flips Healthy and carries its error.
func TestAdminHealthReportsDownDependency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"db":"ok","redis":"down","auth":"ok","detail":{"redis_error":"dial tcp: connection refused"}}`))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminHealth(context.Background())
	if err != nil {
		t.Fatalf("AdminHealth: %v", err)
	}
	if got.Healthy() {
		t.Fatal("Healthy() = true despite redis down")
	}
	if got.Detail.RedisError == "" {
		t.Fatal("RedisError empty; the degraded reason did not decode")
	}
}

// TestAdminHealthAttachesOperatorBearer proves the operator principal is
// authenticated on the operator-guarded read: the request carries the token from
// the single TokenSource. Without it, go-api's OperatorOnly guard would 403.
func TestAdminHealthAttachesOperatorBearer(t *testing.T) {
	var gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write([]byte(`{"db":"ok","redis":"ok","auth":"ok"}`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).AdminHealth(context.Background()); err != nil {
		t.Fatalf("AdminHealth: %v", err)
	}
	if want := "Bearer " + testToken; gotAuth != want {
		t.Fatalf("Authorization = %q, want %q", gotAuth, want)
	}
	if gotAccept != "application/json" {
		t.Fatalf("Accept = %q, want application/json", gotAccept)
	}
}

// TestAdminHealthUnreachableYieldsSourceDown is the other Done proof: an
// unreachable go-api yields the typed source-down error buckets branch on to
// serve last-known state flagged stale.
func TestAdminHealthUnreachableYieldsSourceDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	_, err := newClient(t, url).AdminHealth(context.Background())
	if err == nil {
		t.Fatal("AdminHealth against a closed server returned nil error")
	}
	if !goapi.IsSourceDown(err) {
		t.Fatalf("error %v (%T) is not a SourceDownError", err, err)
	}
	var sd *goapi.SourceDownError
	if !errors.As(err, &sd) || sd.Err == nil {
		t.Fatalf("SourceDownError did not wrap the transport error: %+v", sd)
	}
}

// TestAdminHealthForbiddenYieldsAPIError proves the auth-rejection path: a
// non-operator principal (or a rejected token) is a reachable-but-refused read,
// so it surfaces as an APIError, NOT source-down. go-api is up; it said no.
func TestAdminHealthForbiddenYieldsAPIError(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"operator access required"}`))
		}))

		_, err := newClient(t, srv.URL).AdminHealth(context.Background())
		srv.Close()
		if goapi.IsSourceDown(err) {
			t.Fatalf("status %d misclassified as source-down: %v", status, err)
		}
		var apiErr *goapi.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error %v (%T) is not an APIError", err, err)
		}
		if apiErr.StatusCode != status {
			t.Fatalf("APIError.StatusCode = %d, want %d", apiErr.StatusCode, status)
		}
	}
}

// TestAdminHealthTimeoutIsSourceDown proves a hung go-api cannot wedge a collect
// cycle: it surfaces as source-down within the configured timeout.
func TestAdminHealthTimeoutIsSourceDown(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-block // never respond within the timeout
	}))
	defer srv.Close()
	defer close(block)

	c, err := goapi.New(srv.URL, goapi.StaticTokenSource(testToken), goapi.WithTimeout(50*time.Millisecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.AdminHealth(context.Background()); !goapi.IsSourceDown(err) {
		t.Fatalf("timeout error = %v, want source-down", err)
	}
}

// TestAdminHealthMalformedBodyIsDecodeError proves a hostile/garbage 2xx body
// fails as a decode error, not a silent zero-value snapshot the bucket would
// mistake for a healthy app.
func TestAdminHealthMalformedBodyIsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).AdminHealth(context.Background()); err == nil {
		t.Fatal("malformed body returned nil error")
	}
}

// TestAdminHealthOversizedBodyIsBounded proves a runaway response body cannot
// exhaust Overseer's memory: the read is capped, so a multi-megabyte reply fails
// rather than being swallowed whole.
func TestAdminHealthOversizedBodyIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"db":"`))                  //nolint:errcheck // test stub
		w.Write([]byte(strings.Repeat("a", 2<<20))) //nolint:errcheck // 2 MiB > cap
		w.Write([]byte(`"}`))                       //nolint:errcheck // test stub
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).AdminHealth(context.Background()); err == nil {
		t.Fatal("oversized body returned nil error; read was not bounded")
	}
}

// TestAdminHealthTokenSourceErrorFailsClosed proves no operator read leaves
// without credentials: a TokenSource error stops the call before any transport,
// so the operator bearer is never sent half-formed.
func TestAdminHealthTokenSourceErrorFailsClosed(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hit = true
	}))
	defer srv.Close()

	c, err := goapi.New(srv.URL, goapi.StaticTokenSource("")) // empty => ErrNoToken
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.AdminHealth(context.Background()); err == nil {
		t.Fatal("empty token source returned nil error")
	}
	if hit {
		t.Fatal("a request was sent despite no token being available")
	}
}

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
