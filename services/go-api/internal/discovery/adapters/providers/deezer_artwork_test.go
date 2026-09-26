package providers

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeezerAdapter_Resolve_500IsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewDeezerAdapter(newTestClient(server.URL))
	art, err := adapter.Resolve(context.Background(), domain.ResultKindTrack, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, ports.ErrArtworkUnavailable) {
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

	adapter := NewDeezerAdapter(newTestClient(server.URL))
	art, err := adapter.Resolve(context.Background(), domain.ResultKindTrack, "Discovery", "Daft Punk", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if art != "https://e-cdns-images.dzcdn.net/images/cover/xl.jpg" {
		t.Errorf("art = %q, want the cover_xl URL", art)
	}
}
