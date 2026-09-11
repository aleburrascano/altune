package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetJSON_non200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	var dst map[string]any
	err := getJSON(context.Background(), srv.Client(), srv.URL, &dst)
	if err == nil || !strings.Contains(err.Error(), "http status 500") {
		t.Fatalf("err = %v, want an http status 500 error", err)
	}
}

func TestGetJSON_malformedBodyIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`<html>not json</html>`))
	}))
	defer srv.Close()

	var dst map[string]any
	if err := getJSON(context.Background(), srv.Client(), srv.URL, &dst); err == nil {
		t.Fatal("expected a decode error on an HTML-instead-of-JSON body, got nil")
	}
}

func TestGetBytes_non200ReturnsStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"detail":"slow down"}`))
	}))
	defer srv.Close()

	status, body, err := getBytes(context.Background(), srv.Client(), srv.URL)
	if err == nil {
		t.Fatal("expected an error on 429, got nil")
	}
	if status != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429 (callers branch on it)", status)
	}
	if !strings.Contains(string(body), "slow down") {
		t.Errorf("body = %q, want the 429 body returned for inspection", body)
	}
}

func TestGetBytesCapped_capsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer srv.Close()

	status, body, err := getBytesCapped(context.Background(), srv.Client(), srv.URL, 10)
	if err != nil {
		t.Fatalf("getBytesCapped: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if len(body) != 10 {
		t.Errorf("len(body) = %d, want the 10-byte cap applied", len(body))
	}
}

func TestGetBytes_transportErrorZeroStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	status, _, err := getBytes(context.Background(), http.DefaultClient, srv.URL)
	if err == nil {
		t.Fatal("expected a transport error against a closed server")
	}
	if status != 0 {
		t.Errorf("status = %d, want 0 on a transport error", status)
	}
}

func TestPostJSON_non200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	var dst map[string]any
	status, err := postJSON(context.Background(), srv.Client(), srv.URL, []byte(`{}`), &dst)
	if err == nil || !strings.Contains(err.Error(), "http status 500") {
		t.Fatalf("err = %v, want an http status 500 error", err)
	}
	if status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (callers branch on it)", status)
	}
}

func TestPostJSON_forwardsBodyAndDecodes(t *testing.T) {
	var posted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		posted = string(b)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	var dst struct {
		OK bool `json:"ok"`
	}
	status, err := postJSON(context.Background(), srv.Client(), srv.URL, []byte(`{"q":1}`), &dst)
	if err != nil {
		t.Fatalf("postJSON: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if posted != `{"q":1}` {
		t.Errorf("posted body = %q, want the payload forwarded", posted)
	}
	if !dst.OK {
		t.Error("want the response decoded into dst")
	}
}

func TestPostBytesCapped_non200ReturnsStatusAndBodyWithoutError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"detail":"boom"}`))
	}))
	defer srv.Close()

	status, body, err := postBytesCapped(context.Background(), srv.Client(), srv.URL, strings.NewReader("q"), 1<<20)
	if err != nil {
		t.Fatalf("postBytesCapped must not gate on status (callers branch on it): %v", err)
	}
	if status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", status)
	}
	if !strings.Contains(string(body), "boom") {
		t.Errorf("body = %q, want the 500 body returned for inspection", body)
	}
}

func TestPostBytesCapped_capsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer srv.Close()

	status, body, err := postBytesCapped(context.Background(), srv.Client(), srv.URL, nil, 10)
	if err != nil {
		t.Fatalf("postBytesCapped: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if len(body) != 10 {
		t.Errorf("len(body) = %d, want the 10-byte cap applied", len(body))
	}
}

func TestWithHeader_emptyValueNotSet(t *testing.T) {
	var gotUA string
	var uaPresent bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, uaPresent = r.Header["X-Custom"]
		gotUA = r.Header.Get("X-Other")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	var dst map[string]any
	err := getJSON(context.Background(), srv.Client(), srv.URL, &dst,
		withHeader("X-Custom", ""),
		withHeader("X-Other", "set"))
	if err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	if uaPresent {
		t.Error("withHeader with an empty value must not set the header")
	}
	if gotUA != "set" {
		t.Errorf("X-Other = %q, want %q", gotUA, "set")
	}
}
