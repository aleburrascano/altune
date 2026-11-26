package deezer_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/providers/providertest"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeezerAdapter_Resolve_500IsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := search.NewDeezerAdapter(providertest.NewClient(server.URL))
	art, err := adapter.Resolve(context.Background(), search.ResultKindTrack, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, search.ErrArtworkUnavailable) {
		t.Errorf("Resolve on HTTP 500 = (%q, %v), want (\"\", ErrArtworkUnavailable)", art, err)
	}
}

func TestDeezerAdapter_Resolve_Hit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"data": [{
				"id": 123456,
				"title": "Discovery",
				"cover_xl": "https://e-cdns-images.dzcdn.net/images/cover/xl.jpg"
			}]
		}`))
	}))
	defer server.Close()

	adapter := search.NewDeezerAdapter(providertest.NewClient(server.URL))
	art, err := adapter.Resolve(context.Background(), search.ResultKindTrack, "Discovery", "Daft Punk", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if art != "https://e-cdns-images.dzcdn.net/images/cover/xl.jpg" {
		t.Errorf("art = %q, want the cover_xl URL", art)
	}
}

func TestDeezerAdapter_Resolve_ItsOwnPlaceholderImageIsAMiss(t *testing.T) {
	for _, placeholder := range []string{
		"https://e-cdns-images.dzcdn.net/images/artist//500x500-000000-80-0-0.jpg",
		"https://e-cdns-images.dzcdn.net/images/artist/d41d8cd98f00b204e9800998ecf8427e/500x500.jpg",
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data":[{"id":1,"picture_xl":"` + placeholder + `"}]}`))
		}))

		art, err := search.NewDeezerAdapter(providertest.NewClient(server.URL)).Resolve(context.Background(), search.ResultKindArtist, "Che", "", "")

		server.Close()
		if art != "" || err != nil {
			t.Errorf("Resolve on the placeholder %q = (%q, %v), want (\"\", nil) so the chain moves to the next source", placeholder, art, err)
		}
	}
}

func TestDeezerAdapter_Resolve_RateLimitIsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	art, err := search.NewDeezerAdapter(providertest.NewClient(server.URL)).Resolve(context.Background(), search.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, search.ErrArtworkUnavailable) {
		t.Errorf("Resolve on HTTP 429 = (%q, %v), want (\"\", ErrArtworkUnavailable)", art, err)
	}
}

func TestDeezerAdapter_Resolve_QuotaErrorBodyIsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"error":{"type":"Exception","message":"Quota limit exceeded","code":4}}`))
	}))
	defer server.Close()

	art, err := search.NewDeezerAdapter(providertest.NewClient(server.URL)).Resolve(context.Background(), search.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, search.ErrArtworkUnavailable) {
		t.Errorf("Resolve on a Deezer quota error body = (%q, %v), want (\"\", ErrArtworkUnavailable)", art, err)
	}
}

func TestDeezerAdapter_Resolve_BadRequestIsAVerifiedMiss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	art, err := search.NewDeezerAdapter(providertest.NewClient(server.URL)).Resolve(context.Background(), search.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || err != nil {
		t.Errorf("Resolve on HTTP 400 = (%q, %v), want (\"\", nil)", art, err)
	}
}

func TestDeezerAdapter_Resolve_NotFoundIsAVerifiedMiss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	art, err := search.NewDeezerAdapter(providertest.NewClient(server.URL)).Resolve(context.Background(), search.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || err != nil {
		t.Errorf("Resolve on HTTP 404 = (%q, %v), want (\"\", nil)", art, err)
	}
}

func TestDeezerAdapter_Resolve_NoMatchIsAVerifiedMiss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[],"total":0}`))
	}))
	defer server.Close()

	art, err := search.NewDeezerAdapter(providertest.NewClient(server.URL)).Resolve(context.Background(), search.ResultKindAlbum, "Nothing", "Nobody", "")
	if art != "" || err != nil {
		t.Errorf("Resolve with no match = (%q, %v), want (\"\", nil)", art, err)
	}
}

func TestDeezerAdapter_Resolve_MalformedBodyIsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{`))
	}))
	defer server.Close()

	art, err := search.NewDeezerAdapter(providertest.NewClient(server.URL)).Resolve(context.Background(), search.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, search.ErrArtworkUnavailable) {
		t.Errorf("Resolve on a truncated body = (%q, %v), want (\"\", ErrArtworkUnavailable)", art, err)
	}
}

func TestDeezerAdapter_Resolve_TransportErrorIsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	serverURL := server.URL
	server.Close()

	art, err := search.NewDeezerAdapter(providertest.NewClient(serverURL)).Resolve(context.Background(), search.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, search.ErrArtworkUnavailable) {
		t.Errorf("Resolve against an unreachable host = (%q, %v), want (\"\", ErrArtworkUnavailable)", art, err)
	}
}

func TestDeezerAdapter_Resolve_CancelledContextIsUnavailableAndKeepsTheCause(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	art, err := search.NewDeezerAdapter(providertest.NewClient(server.URL)).Resolve(ctx, search.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, search.ErrArtworkUnavailable) || !errors.Is(err, context.Canceled) {
		t.Errorf("Resolve with a cancelled context = (%q, %v), want (\"\", ErrArtworkUnavailable wrapping context.Canceled)", art, err)
	}
}

func TestDeezerAdapter_Resolve_FailureNamesDeezer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	_, err := search.NewDeezerAdapter(providertest.NewClient(server.URL)).Resolve(context.Background(), search.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if err == nil || !strings.Contains(err.Error(), "deezer") {
		t.Errorf("err = %v, want a failure that names deezer", err)
	}
}
