package requeststore

import (
	"altune/go-api/internal/shared/logging"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func truncatingServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte("0123456789"))
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestCorrelatedTransportRecordsBodyReadError(t *testing.T) {
	server := truncatingServer(t)
	store := New()
	client := &http.Client{Transport: NewCorrelatedTransport(nil, store)}
	ctx := logging.WithCorrelationID(context.Background(), "corr-read-err")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil || resp == nil {
		t.Fatal(err)
	}
	if _, readErr := io.ReadAll(resp.Body); readErr == nil {
		t.Fatal("expected the caller to see the read error")
	}
	_ = resp.Body.Close()
	record, found := store.Get("corr-read-err")
	if !found {
		t.Fatal("record not found")
	}
	if len(record.Exchanges) != 1 || record.Exchanges[0].Err == "" {
		t.Fatalf("exchange Err not recorded: %+v", record.Exchanges)
	}
}

func TestRerunRecorderRecordsBodyReadError(t *testing.T) {
	server := truncatingServer(t)
	recorder := NewRerunRecorder(nil, 1024)
	client := &http.Client{Transport: recorder}
	resp, err := client.Get(server.URL)
	if err != nil || resp == nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	exchanges := recorder.Exchanges()
	if len(exchanges) != 1 || exchanges[0].Err == "" {
		t.Fatalf("exchange Err not recorded: %+v", exchanges)
	}
}
