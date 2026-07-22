package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// newTestAmazonMusicAdapter seeds a fake session so tests exercise showSearch
// without needing a live config.json round trip — the same shape as
// newTestSoundCloudAPI seeding a client_id.
func newTestAmazonMusicAdapter(srv *httptest.Server) *AmazonMusicAdapter {
	a := NewAmazonMusicAdapter(srv.Client())
	a.searchURL = srv.URL
	a.resolver.cached = &amazonMusicSession{
		DeviceID:  "test-device",
		SessionID: "test-session",
		Version:   "1.0.0",
	}
	a.resolver.cached.CSRF.Token = "test-csrf-token"
	return a
}

func allKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

// amzFixtureResponse is a trimmed showSearch response with one track, one
// album, and one artist card, plus a duplicate track (same album+track pair,
// as the real response repeats a top hit in more than one row) to exercise
// dedup. Shape captured from the live web player (2026-07-22).
const amzFixtureResponse = `{
  "methods": [{
    "template": {
      "widgets": [{
        "items": [
          {
            "interface": "Web.TemplatesInterface.v1_0.Touch.WidgetsInterface.SquareHorizontalItemElement",
            "primaryText": {"text": "Blinding Lights"},
            "secondaryText": "The Weeknd",
            "image": "https://m.media-amazon.com/images/I/track.jpg",
            "primaryLink": {"deeplink": "/albums/B086Q2QNLH?trackAsin=B086Q41M9C"},
            "secondaryLink": {"deeplink": "/artists/B00G9Y64K6/the-weeknd"}
          },
          {
            "interface": "Web.TemplatesInterface.v1_0.Touch.WidgetsInterface.SquareHorizontalItemElement",
            "primaryText": {"text": "Blinding Lights"},
            "secondaryText": "The Weeknd",
            "image": "https://m.media-amazon.com/images/I/track.jpg",
            "primaryLink": {"deeplink": "/albums/B086Q2QNLH?trackAsin=B086Q41M9C"},
            "secondaryLink": {"deeplink": "/artists/B00G9Y64K6/the-weeknd"}
          },
          {
            "interface": "Web.TemplatesInterface.v1_0.Touch.WidgetsInterface.SquareVerticalItemElement",
            "primaryText": {"text": "After Hours (Deluxe)"},
            "secondaryText": "The Weeknd",
            "image": "https://m.media-amazon.com/images/I/album.jpg",
            "primaryLink": {"deeplink": "/albums/B086Q2QNLH"},
            "secondaryLink": {"deeplink": "/artists/B00G9Y64K6/the-weeknd"}
          },
          {
            "interface": "Web.TemplatesInterface.v1_0.Touch.WidgetsInterface.CircleVerticalItemElement",
            "primaryText": {"text": "The Weeknd"},
            "image": "https://m.media-amazon.com/images/I/artist.jpg",
            "primaryLink": {"deeplink": "/artists/B00G9Y64K6/the-weeknd"}
          },
          {
            "interface": "Web.TemplatesInterface.v1_0.Touch.WidgetsInterface.SquareVerticalItemElement",
            "primaryText": {"text": "Some Podcast Episode"},
            "image": "https://m.media-amazon.com/images/I/podcast.jpg",
            "primaryLink": {"deeplink": "/podcasts/B0PODCAST1"}
          }
        ]
      }]
    }
  }]
}`

func TestAmazonMusicAdapter_Search_classifiesAndDedupes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(amzFixtureResponse))
	}))
	defer srv.Close()

	a := newTestAmazonMusicAdapter(srv)
	results, err := a.Search(t.Context(), "Blinding Lights", allKinds())
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	byKind := map[domain.ResultKind][]domain.SearchResult{}
	for _, r := range results {
		byKind[r.Kind] = append(byKind[r.Kind], r)
	}

	if got := len(byKind[domain.ResultKindTrack]); got != 1 {
		t.Errorf("track count = %d, want 1 (duplicate should dedupe)", got)
	}
	if got := len(byKind[domain.ResultKindAlbum]); got != 1 {
		t.Errorf("album count = %d, want 1", got)
	}
	if got := len(byKind[domain.ResultKindArtist]); got != 1 {
		t.Errorf("artist count = %d, want 1", got)
	}
	// The podcast card carries no /albums or /artists deeplink and must be
	// dropped, not misclassified.
	for _, r := range results {
		if r.Title == "Some Podcast Episode" {
			t.Errorf("podcast card leaked into results: %+v", r)
		}
	}

	track := byKind[domain.ResultKindTrack][0]
	if track.Title != "Blinding Lights" || track.Subtitle != "The Weeknd" {
		t.Errorf("track = %+v, want title/subtitle Blinding Lights/The Weeknd", track)
	}
	if len(track.Sources) != 1 || track.Sources[0].ExternalID != "B086Q41M9C" {
		t.Errorf("track source = %+v, want ExternalID B086Q41M9C (the trackAsin, not the album)", track.Sources)
	}
	if track.Extras["album_asin"] != "B086Q2QNLH" {
		t.Errorf("track album_asin extra = %v, want B086Q2QNLH", track.Extras["album_asin"])
	}
	if track.Extras["artist_asin"] != "B00G9Y64K6" {
		t.Errorf("track artist_asin extra = %v, want B00G9Y64K6", track.Extras["artist_asin"])
	}

	album := byKind[domain.ResultKindAlbum][0]
	if len(album.Sources) != 1 || album.Sources[0].ExternalID != "B086Q2QNLH" {
		t.Errorf("album source = %+v, want ExternalID B086Q2QNLH", album.Sources)
	}

	artist := byKind[domain.ResultKindArtist][0]
	if len(artist.Sources) != 1 || artist.Sources[0].ExternalID != "B00G9Y64K6" {
		t.Errorf("artist source = %+v, want ExternalID B00G9Y64K6", artist.Sources)
	}
}

func TestAmazonMusicAdapter_Search_filtersByRequestedKinds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(amzFixtureResponse))
	}))
	defer srv.Close()

	a := newTestAmazonMusicAdapter(srv)
	results, err := a.Search(t.Context(), "Blinding Lights", map[domain.ResultKind]bool{domain.ResultKindArtist: true})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	for _, r := range results {
		if r.Kind != domain.ResultKindArtist {
			t.Errorf("got kind %v, want only artist results", r.Kind)
		}
	}
	if len(results) != 1 {
		t.Errorf("results = %d, want 1 artist", len(results))
	}
}

func TestAmazonMusicAdapter_Search_reResolvesSessionOnAuthFailure(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(amzFixtureResponse))
	}))
	defer srv.Close()

	// A second server plays config.json so invalidate() can re-resolve.
	configSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"deviceId":  "fresh-device",
			"sessionId": "fresh-session",
			"version":   "1.0.0",
			"csrf":      map[string]string{"token": "fresh-token", "rnd": "1", "ts": "1"},
		})
	}))
	defer configSrv.Close()

	a := newTestAmazonMusicAdapter(srv)
	a.resolver.configURL = configSrv.URL

	results, err := a.Search(t.Context(), "Blinding Lights", allKinds())
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (initial 403 then retry after re-resolve)", calls)
	}
	if len(results) == 0 {
		t.Errorf("expected results after re-resolve, got none")
	}
}

func TestAmazonMusicSessionResolver_resolve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"deviceId":  "d1",
			"sessionId": "s1",
			"version":   "1.2.3",
			"csrf":      map[string]string{"token": "t1", "rnd": "r1", "ts": "ts1"},
		})
	}))
	defer srv.Close()

	r := newAmazonMusicSessionResolver(srv.Client())
	r.configURL = srv.URL

	sess, err := r.get(t.Context())
	if err != nil {
		t.Fatalf("get() error = %v", err)
	}
	if sess.DeviceID != "d1" || sess.SessionID != "s1" || sess.CSRF.Token != "t1" {
		t.Errorf("session = %+v, want d1/s1/t1", sess)
	}
}

func TestAmazonMusicAdapter_Name(t *testing.T) {
	a := NewAmazonMusicAdapter(http.DefaultClient)
	if got := a.Name(); got != domain.ProviderAmazonMusic {
		t.Errorf("Name() = %v, want %v", got, domain.ProviderAmazonMusic)
	}
}
