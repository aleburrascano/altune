package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func coverServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/blank":
			http.Redirect(w, r, "/images/cover/"+deezerPlaceholderImageID+"/1000x1000.jpg", http.StatusFound)
		case "/real":
			http.Redirect(w, r, "/images/cover/abc123/1000x1000.jpg", http.StatusFound)
		case "/images/cover/abc123/1000x1000.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
		case "/page":
			w.Header().Set("Content-Type", "text/html")
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestHTTPCoverProbe_RecognisesTheDeezerPlaceholderByItsRedirectWithoutFollowingIt(t *testing.T) {
	srv := coverServer()
	defer srv.Close()
	probe := newHTTPCoverProbe(srv.Client(), 1000)
	for path, want := range map[string]bool{"/blank": true, "/real": false, "/missing": false} {
		if got, err := probe.IsPlaceholder(t.Context(), srv.URL+path); err != nil || got != want {
			t.Errorf("%s: got %v, %v", path, got, err)
		}
	}
}

func TestHTTPCoverProbe_AnArtworkUrlResolvesOnlyWhenItEndsInAnImage(t *testing.T) {
	srv := coverServer()
	defer srv.Close()
	probe := newHTTPCoverProbe(srv.Client(), 1000)
	for path, want := range map[string]bool{"/real": true, "/page": false, "/missing": false} {
		if got, err := probe.Resolves(t.Context(), srv.URL+path); err != nil || got != want {
			t.Errorf("%s: got %v, %v", path, got, err)
		}
	}
}
