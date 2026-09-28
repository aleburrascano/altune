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

const testToken = "operator-jwt-token-value"

func TestHealthDecodesStubbedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("stub got method %s, want GET", r.Method)
		}
		if r.URL.Path != "/health" {
			t.Errorf("stub got path %s, want /health", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := newClient(t, srv.URL)
	got, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: unexpected error: %v", err)
	}
	if !got.OK() || got.Status != "ok" {
		t.Fatalf("Health = %+v, want status ok", got)
	}
}

func TestAttachesOperatorBearer(t *testing.T) {
	var gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
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

func TestUnreachableYieldsSourceDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := newClient(t, url)
	_, err := c.Health(context.Background())
	if err == nil {
		t.Fatal("Health against a closed server returned nil error")
	}
	if !goapi.IsSourceDown(err) {
		t.Fatalf("error %v (%T) is not a SourceDownError", err, err)
	}
	var sd *goapi.SourceDownError
	if !errors.As(err, &sd) || sd.Err == nil {
		t.Fatalf("SourceDownError did not wrap the transport error: %+v", sd)
	}
}

func TestSendsCorrelationID(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Correlation-ID")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if got == "" {
		t.Fatal("no X-Correlation-ID sent")
	}
	if len(got) > 64 {
		t.Fatalf("X-Correlation-ID %q exceeds go-api's 64-char cap; it would be rejected", got)
	}
	for _, c := range got {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			t.Fatalf("X-Correlation-ID %q has a char go-api rejects: %q", got, c)
		}
	}
}

func TestAPIErrorRecordsEchoedCorrelationID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Correlation-ID", "srvecho01")
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := newClient(t, srv.URL).Health(context.Background())
	var apiErr *goapi.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v (%T) is not an APIError", err, err)
	}
	if apiErr.CorrID != "srvecho01" {
		t.Fatalf("APIError.CorrID = %q, want srvecho01 (echoed off the response)", apiErr.CorrID)
	}
}

func TestSourceDownRecordsOutboundCorrelationID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	_, err := newClient(t, url).Health(context.Background())
	var sd *goapi.SourceDownError
	if !errors.As(err, &sd) {
		t.Fatalf("error %v (%T) is not a SourceDownError", err, err)
	}
	if sd.CorrID == "" {
		t.Fatal("SourceDownError.CorrID is empty; the outbound correlation id was not recorded")
	}
}

func TestTimeoutIsSourceDown(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	c, err := goapi.New(srv.URL, goapi.StaticTokenSource(testToken), goapi.WithTimeout(50*time.Millisecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Health(context.Background()); !goapi.IsSourceDown(err) {
		t.Fatalf("timeout error = %v, want source-down", err)
	}
}

func TestNon2xxYieldsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"reason":"expired"}`))
	}))
	defer srv.Close()

	_, err := newClient(t, srv.URL).Health(context.Background())
	if goapi.IsSourceDown(err) {
		t.Fatalf("401 misclassified as source-down: %v", err)
	}
	var apiErr *goapi.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v (%T) is not an APIError", err, err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("APIError.StatusCode = %d, want 401", apiErr.StatusCode)
	}
}

func TestMalformedBodyIsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).Health(context.Background()); err == nil {
		t.Fatal("malformed body returned nil error")
	}
}

func TestOversizedBodyIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"`))
		_, _ = w.Write([]byte(strings.Repeat("a", 2<<20)))
		_, _ = w.Write([]byte(`"}`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).Health(context.Background()); err == nil {
		t.Fatal("oversized body returned nil error; read was not bounded")
	}
}

func TestTokenSourceErrorFailsClosed(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hit = true
	}))
	defer srv.Close()

	c, err := goapi.New(srv.URL, goapi.StaticTokenSource(""))
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

func TestNewValidatesConfig(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		tokens  goapi.TokenSource
	}{
		{"empty base url", "", goapi.StaticTokenSource(testToken)},
		{"no scheme", "api.altune.app", goapi.StaticTokenSource(testToken)},
		{"unsupported scheme", "ftp://api.altune.app", goapi.StaticTokenSource(testToken)},
		{"missing host", "https://", goapi.StaticTokenSource(testToken)},
		{"nil token source", "https://api.altune.app", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := goapi.New(tc.baseURL, tc.tokens); err == nil {
				t.Fatalf("New(%q) returned nil error, want rejection", tc.baseURL)
			}
		})
	}
}

func newClient(t *testing.T, baseURL string) *goapi.Client {
	t.Helper()
	c, err := goapi.New(baseURL, goapi.StaticTokenSource(testToken))
	if err != nil {
		t.Fatalf("New(%q): %v", baseURL, err)
	}
	return c
}
