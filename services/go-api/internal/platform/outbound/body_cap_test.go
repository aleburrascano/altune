package outbound_test

import (
	"altune/go-api/internal/platform/outbound"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func chunkedServer(t *testing.T, size int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, strings.Repeat("x", size/2))
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = io.WriteString(w, strings.Repeat("x", size-size/2))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func readAll(t *testing.T, client *http.Client, target string) ([]byte, error) {
	t.Helper()
	resp, err := client.Get(target)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

func TestNewClient_WithBodyCapFailsAStreamedBodyOverTheCap(t *testing.T) {
	srv := chunkedServer(t, 100)
	client := outbound.MustNewClient(time.Second, nil, outbound.WithBodyCap(64))

	got, err := readAll(t, client, srv.URL)

	if !errors.Is(err, outbound.ErrBodyTooLarge) {
		t.Fatalf("err = %v, want ErrBodyTooLarge", err)
	}
	if len(got) > 64 {
		t.Fatalf("read %d bytes, want at most the cap of 64", len(got))
	}
}

func TestNewClient_WithBodyCapPassesABodyAtTheCap(t *testing.T) {
	srv := chunkedServer(t, 64)
	client := outbound.MustNewClient(time.Second, nil, outbound.WithBodyCap(64))

	got, err := readAll(t, client, srv.URL)

	if err != nil || len(got) != 64 {
		t.Fatalf("got %d bytes, err %v, want 64 bytes and no error", len(got), err)
	}
}

func TestNewClient_WithBodyCapRefusesAnAnnouncedLengthOverTheCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 100))
	}))
	t.Cleanup(srv.Close)
	client := outbound.MustNewClient(time.Second, nil, outbound.WithBodyCap(64))

	_, err := readAll(t, client, srv.URL)

	if !errors.Is(err, outbound.ErrBodyTooLarge) {
		t.Fatalf("err = %v, want ErrBodyTooLarge", err)
	}
}

func TestNewClient_WithoutABodyCapReadsTheWholeBody(t *testing.T) {
	srv := chunkedServer(t, 4096)
	client := outbound.MustNewClient(time.Second, nil)

	got, err := readAll(t, client, srv.URL)

	if err != nil || len(got) != 4096 {
		t.Fatalf("got %d bytes, err %v, want 4096 bytes and no error", len(got), err)
	}
}

func TestNewClient_RefusesANonPositiveBodyCap(t *testing.T) {
	for _, limit := range []int64{0, -1} {
		if _, err := outbound.NewClient(time.Second, nil, outbound.WithBodyCap(limit)); err == nil {
			t.Fatalf("body cap %d was accepted", limit)
		}
	}
}

func TestDoCapped_ReturnsAnErrorStatusWithItsBodyAndNoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, "short and stout")
	}))
	t.Cleanup(srv.Close)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	status, body, err := outbound.DoCapped(srv.Client(), req, 5)

	if err != nil || status != http.StatusTeapot || string(body) != "short" {
		t.Fatalf("status %d body %q err %v, want 418, the first 5 bytes and no error", status, body, err)
	}
}
