package main

import (
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func coverServer(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	images := map[string]image.Image{
		"/plain":     gradient(80, false),
		"/plain-big": gradient(240, false),
		"/inverted":  gradient(80, true),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/garbage" {
			_, _ = w.Write([]byte("not an image"))
			return
		}
		img, ok := images[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if err := png.Encode(w, img); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestCoverComparer_SameArtworkAtAnotherResolutionIsTheSameImage(t *testing.T) {
	var hits atomic.Int32
	server := coverServer(t, &hits)
	same, err := newCoverComparer(server.Client()).SameImage(context.Background(), server.URL+"/plain", server.URL+"/plain-big")
	if err != nil || !same {
		t.Fatalf("same %v err %v", same, err)
	}
}

func TestCoverComparer_DifferentArtworkIsNotTheSameImage(t *testing.T) {
	var hits atomic.Int32
	server := coverServer(t, &hits)
	same, err := newCoverComparer(server.Client()).SameImage(context.Background(), server.URL+"/plain", server.URL+"/inverted")
	if err != nil || same {
		t.Fatalf("same %v err %v", same, err)
	}
}

func TestCoverComparer_FetchesEachUrlOnce(t *testing.T) {
	var hits atomic.Int32
	server := coverServer(t, &hits)
	comparer := newCoverComparer(server.Client())
	for range 3 {
		if _, err := comparer.SameImage(context.Background(), server.URL+"/plain", server.URL+"/inverted"); err != nil {
			t.Fatal(err)
		}
	}
	if hits.Load() != 2 {
		t.Fatalf("hits %d", hits.Load())
	}
}

func TestCoverComparer_UnreadableCoverIsAnError(t *testing.T) {
	var hits atomic.Int32
	server := coverServer(t, &hits)
	comparer := newCoverComparer(server.Client())
	for _, path := range []string{"/garbage", "/missing"} {
		if _, err := comparer.SameImage(context.Background(), server.URL+"/plain", server.URL+path); err == nil {
			t.Fatalf("%s: expected an error", path)
		}
	}
}

func TestCoverURLContainsImage(t *testing.T) {
	if !coverURLContainsImage("https://cdn/images/cover/abc123/500x500.jpg", "abc123") {
		t.Fatal("expected a match")
	}
	if coverURLContainsImage("https://cdn/x.jpg", "") {
		t.Fatal("empty md5 must not match")
	}
}
