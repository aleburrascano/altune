package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFanartTVSendsTheAdapterRequests(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(r.Context())
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	src := newFanartTVSource(srv.Client(), srv.URL+"/v3/", "k")
	cases := []struct{ entity, uri string }{
		{"artist", "/v3/music/09d6b57e-9cd9-4a74-b7cb-842b8443a1cd?api_key=k"},
		{"album", "/v3/music/albums/09d6b57e-9cd9-4a74-b7cb-842b8443a1cd?api_key=k"},
	}
	for _, c := range cases {
		if _, err := src.Resource(context.Background(), c.entity, "09d6b57e-9cd9-4a74-b7cb-842b8443a1cd"); err != nil {
			t.Fatalf("%s: %v", c.entity, err)
		}
		if got == nil {
			t.Fatalf("%s: no request reached the server", c.entity)
		}
		if got.Method != http.MethodGet || got.URL.RequestURI() != c.uri {
			t.Errorf("%s: got %s %s, want %s", c.entity, got.Method, got.URL.RequestURI(), c.uri)
		}
	}
}

func TestFanartTVFailsOnAnErrorStatusWithoutTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	_, err := newFanartTVSource(srv.Client(), srv.URL+"/v3/", "secret").Resource(context.Background(), "album", "x")
	if err == nil || !strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("got %v", err)
	}
}

func TestFanartTVProviderNeedsTheKey(t *testing.T) {
	t.Setenv("FANARTTV_API_KEY", "")
	if _, err := fanartTVProvider().source(http.DefaultClient); err == nil {
		t.Fatal("want an error")
	}
}
