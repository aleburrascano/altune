package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestSoundCloudAPIAdapter_GetRelatedTracks_MapsCollection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tracks/12345/related" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.URL.Query().Get("client_id") == "" {
			t.Error("expected client_id query param")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"collection":[
			{"id":555,"kind":"track","title":"Fell In Love","permalink_url":"https://soundcloud.com/x/fell-in-love",
			 "duration":150000,"genre":"Rap","playback_count":12000,"user":{"username":"Lil Tecca"}},
			{"id":556,"kind":"track","title":"Collab Leak","user":{"username":"Ken Carson"}},
			{"id":0,"title":"skip — no id"},
			{"id":7,"kind":"playlist","title":"skip — not a track"}
		]}`))
	}))
	defer srv.Close()

	a := newTestSoundCloudAPI(srv, nil)
	results, err := a.GetRelatedTracks(context.Background(), domain.ProviderSoundCloud, "12345")
	if err != nil {
		t.Fatalf("GetRelatedTracks error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 mapped tracks (others skipped), got %d", len(results))
	}

	first := results[0]
	if first.Kind != domain.ResultKindTrack {
		t.Errorf("kind = %v, want track", first.Kind)
	}
	if first.Title != "Fell In Love" || first.Subtitle != "Lil Tecca" {
		t.Errorf("first mapped wrong: %+v", first)
	}
	if len(first.Sources) != 1 ||
		first.Sources[0].Provider != domain.ProviderSoundCloud ||
		first.Sources[0].ExternalID != "555" {
		t.Errorf("source not soundcloud/555: %+v", first.Sources)
	}
	if first.Extras["genre"] != "Rap" {
		t.Errorf("genre extra missing: %+v", first.Extras)
	}
	if got := first.Extras["playback_count"]; got != int64(12000) {
		t.Errorf("playback_count = %v (%T), want int64 12000", got, got)
	}
}

func TestSoundCloudAPIAdapter_GetRelatedTracks_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"collection":[]}`))
	}))
	defer srv.Close()

	a := newTestSoundCloudAPI(srv, nil)
	results, err := a.GetRelatedTracks(context.Background(), domain.ProviderSoundCloud, "999")
	if err != nil {
		t.Fatalf("GetRelatedTracks error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected empty result set, got %d", len(results))
	}
}
