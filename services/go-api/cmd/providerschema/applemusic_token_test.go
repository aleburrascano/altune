package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testJWT = "eyJhbGciOiJFUzI1NiJ9.eyJpc3MiOiJ4In0.c2ln"

func appleServer(t *testing.T, bundle string, catalog http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/us/search", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<script src="/assets/index~abc123.js"></script>`))
	})
	mux.HandleFunc("/assets/index~abc123.js", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(bundle))
	})
	mux.HandleFunc("/v1/catalog/us/", catalog)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestAppleWebCatalogFetchesTheResourceWithTheBundleToken(t *testing.T) {
	var auth, origin, agent string
	srv := appleServer(t, `x="`+testJWT+`"`, func(w http.ResponseWriter, r *http.Request) {
		auth, origin, agent = r.Header.Get("Authorization"), r.Header.Get("Origin"), r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"data":[{"id":"9","attributes":{"isCompilation":true}}]}`))
	})
	raw, err := newAppleMusicCatalog(srv.Client(), srv.URL, srv.URL+"/v1/catalog/us").CatalogResource(context.Background(), "albums", "9")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"id":"9","attributes":{"isCompilation":true}}` || auth != "Bearer "+testJWT {
		t.Fatalf("got %s with %q", raw, auth)
	}
	if origin != srv.URL || agent != appleMusicUserAgent {
		t.Fatalf("sent origin %q and user agent %q", origin, agent)
	}
}

func TestAppleWebCatalogFailsWhenTheBundleHasNoToken(t *testing.T) {
	srv := appleServer(t, `no token here`, func(http.ResponseWriter, *http.Request) {})
	if _, err := newAppleMusicCatalog(srv.Client(), srv.URL, srv.URL+"/v1/catalog/us").CatalogResource(context.Background(), "albums", "9"); err == nil {
		t.Fatal("want an error")
	}
}

func TestAppleWebCatalogFailsOnAnEmptyResponse(t *testing.T) {
	srv := appleServer(t, testJWT, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	if _, err := newAppleMusicCatalog(srv.Client(), srv.URL, srv.URL+"/v1/catalog/us").CatalogResource(context.Background(), "albums", "9"); err == nil {
		t.Fatal("want an error")
	}
}

func TestAppleWebCatalogFailsOnAnErrorStatus(t *testing.T) {
	srv := appleServer(t, testJWT, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	if _, err := newAppleMusicCatalog(srv.Client(), srv.URL, srv.URL+"/v1/catalog/us").CatalogResource(context.Background(), "albums", "9"); err == nil {
		t.Fatal("want an error")
	}
}
