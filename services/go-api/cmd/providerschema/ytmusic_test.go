package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type ytMusicRequest struct {
	method, path, key, contentType, referer, userAgent string
	body                                               map[string]any
}

func ytMusicServer(t *testing.T, status int) (*httptest.Server, *ytMusicRequest) {
	t.Helper()
	got := &ytMusicRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.key = r.Method, r.URL.Path, r.URL.Query().Get("key")
		got.contentType, got.referer, got.userAgent = r.Header.Get("Content-Type"), r.Header.Get("Referer"), r.Header.Get("User-Agent")
		if err := json.NewDecoder(r.Body).Decode(&got.body); err != nil {
			t.Errorf("the adapter sent a body that is not JSON: %v", err)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"contents":{}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func TestYTMusicSearchPostsTheInnertubeRequestTheAdapterSends(t *testing.T) {
	srv, got := ytMusicServer(t, http.StatusOK)
	raw, err := newYTMusicSource(srv.Client(), srv.URL).Resource(context.Background(), "search_album", "Nia Archives")
	if err != nil || string(raw) != `{"contents":{}}` {
		t.Fatalf("got %s, %v", raw, err)
	}
	if got.method != http.MethodPost || got.path != "/youtubei/v1/search" || got.key == "" {
		t.Fatalf("sent %s %s with key %q", got.method, got.path, got.key)
	}
	if got.contentType != "application/json; charset=utf-8" || got.referer != "https://music.youtube.com/search" || got.userAgent == "" {
		t.Fatalf("sent headers %q %q %q", got.contentType, got.referer, got.userAgent)
	}
	client, _ := got.body["context"].(map[string]any)["client"].(map[string]any)
	if got.body["query"] != "Nia Archives" || got.body["params"] != ytMusicEndpoints["search_album"].filter || client["clientName"] != "WEB_REMIX" {
		t.Fatalf("sent body %v", got.body)
	}
}

func TestYTMusicUnfilteredSearchSendsNoParams(t *testing.T) {
	srv, got := ytMusicServer(t, http.StatusOK)
	if _, err := newYTMusicSource(srv.Client(), srv.URL).Resource(context.Background(), "search", "Subnation"); err != nil {
		t.Fatal(err)
	}
	if _, ok := got.body["params"]; ok || got.body["query"] != "Subnation" {
		t.Fatalf("sent body %v", got.body)
	}
}

func TestYTMusicSearchFailsOnAnErrorStatus(t *testing.T) {
	srv, _ := ytMusicServer(t, http.StatusTooManyRequests)
	if _, err := newYTMusicSource(srv.Client(), srv.URL).Resource(context.Background(), "search_artist", "sombr"); err == nil {
		t.Fatal("want an error")
	}
}
