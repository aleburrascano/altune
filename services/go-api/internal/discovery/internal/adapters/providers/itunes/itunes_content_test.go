package itunes_test

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/discovery/internal/adapters/providers/providertest"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestITunesAdapter_GetArtistTopTracks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lookup" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("entity"); got != "song" {
			t.Errorf("entity = %q, want song", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resultCount": 2, "results": [
			{"wrapperType": "artist", "artistName": "Che"},
			{"wrapperType": "track", "trackId": 111, "trackName": "Los Santos", "artistName": "Che", "trackTimeMillis": 125000}
		]}`))
	}))
	defer server.Close()

	adapter := discovery.NewITunesAdapter(providertest.NewClient(server.URL))
	tracks, err := adapter.GetArtistTopTracks(context.Background(), discovery.ProviderITunes, "42")
	if err != nil {
		t.Fatalf("GetArtistTopTracks: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("tracks = %d, want 1 (parent wrapper dropped)", len(tracks))
	}
	if tracks[0].Kind != discovery.ResultKindTrack || tracks[0].Title != "Los Santos" {
		t.Errorf("track = %+v", tracks[0])
	}
}
