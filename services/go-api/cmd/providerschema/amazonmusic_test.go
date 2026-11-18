package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const amazonMusicTestConfig = `{"deviceId":"dev-1","sessionId":"sess-1","version":"1.0.1"}`

func amazonMusicServer(t *testing.T, status int) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	var got []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Clone(context.Background()))
		w.WriteHeader(status)
		_, _ = w.Write([]byte(amazonMusicTestConfig))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestAmazonMusicConfigIsAPlainGetWithTheAdaptersUserAgent(t *testing.T) {
	srv, got := amazonMusicServer(t, http.StatusOK)
	raw, err := newAmazonMusicSource(srv.Client(), srv.URL).Resource(context.Background(), "config", "web")
	if err != nil || string(raw) != amazonMusicTestConfig {
		t.Fatalf("got %s, %v", raw, err)
	}
	r := (*got)[0]
	if r.Method != http.MethodGet || r.URL.RequestURI() != "/config.json" || r.Header.Get("User-Agent") != amazonMusicUserAgent {
		t.Fatalf("sent %s %s with %q", r.Method, r.URL.RequestURI(), r.Header.Get("User-Agent"))
	}
}

func TestAmazonMusicConfigFailsOnAnErrorStatus(t *testing.T) {
	srv, _ := amazonMusicServer(t, http.StatusForbidden)
	if _, err := newAmazonMusicSource(srv.Client(), srv.URL).Resource(context.Background(), "config", "web"); err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("got %v", err)
	}
}
