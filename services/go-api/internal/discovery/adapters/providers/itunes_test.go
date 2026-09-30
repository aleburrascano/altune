package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

import (
	"altune/go-api/internal/discovery/ports"
	"errors"
)

func TestITunesAdapter_Search_Tracks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/search") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": [{
				"trackId": 456789,
				"trackName": "Stairway to Heaven",
				"artistName": "Led Zeppelin",
				"collectionName": "Led Zeppelin IV",
				"trackViewUrl": "https://music.apple.com/track/456789",
				"artworkUrl100": "https://is1-ssl.mzstatic.com/image/100x100.jpg",
				"trackTimeMillis": 482000,
				"primaryGenreName": "Rock"
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewITunesAdapter(newTestClient(server.URL))
	results, err := adapter.Search(context.Background(), "stairway to heaven", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindTrack {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindTrack)
	}
	if r.Title != "Stairway to Heaven" {
		t.Errorf("title: got %q, want %q", r.Title, "Stairway to Heaven")
	}
	if r.Subtitle != "Led Zeppelin" {
		t.Errorf("subtitle: got %q, want %q", r.Subtitle, "Led Zeppelin")
	}
	if !strings.Contains(r.ImageURL, "600x600") {
		t.Errorf("imageURL should contain 600x600, got %q", r.ImageURL)
	}
	if r.Confidence != domain.ConfidenceLow {
		t.Errorf("confidence: got %v, want %v", r.Confidence, domain.ConfidenceLow)
	}
	if len(r.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(r.Sources))
	}
	if r.Sources[0].Provider != domain.ProviderITunes {
		t.Errorf("source provider: got %v, want %v", r.Sources[0].Provider, domain.ProviderITunes)
	}
	if r.Sources[0].ExternalID != "456789" {
		t.Errorf("source externalID: got %q, want %q", r.Sources[0].ExternalID, "456789")
	}
	if r.Sources[0].URL != "https://music.apple.com/track/456789" {
		t.Errorf("source URL: got %q, want apple music URL", r.Sources[0].URL)
	}
	if r.Extras["album"] != "Led Zeppelin IV" {
		t.Errorf("extras.album: got %v, want %q", r.Extras["album"], "Led Zeppelin IV")
	}
	if dur, ok := r.Extras["duration"].(int64); !ok || dur != 482 {
		t.Errorf("extras.duration: got %v (%T), want 482", r.Extras["duration"], r.Extras["duration"])
	}
	if r.Extras["genre"] != "Rock" {
		t.Errorf("extras.genre: got %v, want %q", r.Extras["genre"], "Rock")
	}
}

func TestITunesAdapter_Search_Artists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": [{
				"trackId": 0,
				"trackName": "",
				"artistName": "Pink Floyd",
				"collectionName": "",
				"trackViewUrl": "https://music.apple.com/artist/pinkfloyd",
				"artworkUrl100": "https://is1-ssl.mzstatic.com/artist/100x100.jpg",
				"trackTimeMillis": 0
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewITunesAdapter(newTestClient(server.URL))
	results, err := adapter.Search(context.Background(), "pink floyd", map[domain.ResultKind]bool{
		domain.ResultKindArtist: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindArtist {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindArtist)
	}
	if r.Title != "Pink Floyd" {
		t.Errorf("title: got %q, want %q", r.Title, "Pink Floyd")
	}
}

func TestITunesAdapter_Resolve_HighRes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": [{
				"collectionName": "Discovery",
				"artistName": "Daft Punk",
				"artworkUrl100": "https://is1-ssl.mzstatic.com/image/100x100bb.jpg"
			}]
		}`))
	}))
	defer server.Close()

	adapter := NewITunesAdapter(newTestClient(server.URL))
	art, err := adapter.Resolve(context.Background(), domain.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !strings.Contains(art, "1500x1500") {
		t.Errorf("hero artwork should be 1500x1500, got %q", art)
	}
}

func TestITunesAdapter_GetArtistAlbums(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": [
				{"wrapperType": "artist", "artistId": 368183298, "artistName": "Kendrick Lamar"},
				{"wrapperType": "collection", "collectionId": 1440881047, "collectionName": "DAMN.", "artistName": "Kendrick Lamar", "trackCount": 15, "releaseDate": "2017-04-14T07:00:00Z", "collectionViewUrl": "https://music.apple.com/album/1440881047", "artworkUrl100": "https://is1-ssl.mzstatic.com/image/100x100bb.jpg"},
				{"wrapperType": "collection", "collectionId": 1781270319, "collectionName": "GNX", "artistName": "Kendrick Lamar", "trackCount": 12}
			]
		}`))
	}))
	defer server.Close()

	adapter := NewITunesAdapter(newTestClient(server.URL))
	results, err := adapter.GetArtistAlbums(context.Background(), domain.ProviderITunes, "368183298")
	if err != nil {
		t.Fatalf("GetArtistAlbums: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 albums, got %d", len(results))
	}
	first := results[0]
	if first.Kind != domain.ResultKindAlbum {
		t.Errorf("kind: got %v, want album", first.Kind)
	}
	if first.Title != "DAMN." {
		t.Errorf("title: got %q, want %q", first.Title, "DAMN.")
	}
	if first.Sources[0].ExternalID != "1440881047" {
		t.Errorf("externalID: got %q, want collectionId 1440881047", first.Sources[0].ExternalID)
	}
	if first.TrackCount != 15 {
		t.Errorf("TrackCount: got %d, want 15", first.TrackCount)
	}
}

func TestITunesAdapter_GetAlbumTracks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": [
				{"wrapperType": "collection", "collectionId": 1440881722, "collectionName": "DAMN.", "artistName": "Kendrick Lamar"},
				{"wrapperType": "track", "trackId": 1440881736, "trackName": "BLOOD.", "artistName": "Kendrick Lamar", "collectionName": "DAMN.", "trackViewUrl": "https://music.apple.com/track/1440881736"},
				{"wrapperType": "track", "trackId": 1440881990, "trackName": "DNA.", "artistName": "Kendrick Lamar", "collectionName": "DAMN."}
			]
		}`))
	}))
	defer server.Close()

	adapter := NewITunesAdapter(newTestClient(server.URL))
	results, err := adapter.GetAlbumTracks(context.Background(), domain.ProviderITunes, "1440881722")
	if err != nil {
		t.Fatalf("GetAlbumTracks: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(results))
	}
	if results[0].Kind != domain.ResultKindTrack {
		t.Errorf("kind: got %v, want track", results[0].Kind)
	}
	if results[0].Title != "BLOOD." {
		t.Errorf("title: got %q, want %q", results[0].Title, "BLOOD.")
	}
	if results[0].Sources[0].ExternalID != "1440881736" {
		t.Errorf("externalID: got %q, want trackId 1440881736", results[0].Sources[0].ExternalID)
	}
}

func TestITunesAdapter_Search_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewITunesAdapter(newTestClient(server.URL))
	results, err := adapter.Search(context.Background(), "anything", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err == nil {
		t.Fatal("expected an error when all attempted kinds fail on HTTP 500, got nil")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results on HTTP 500, got %d", len(results))
	}
}

func TestStripAlbumTypeSuffix(t *testing.T) {
	cases := map[string]string{
		"Fully Loaded - EP":                    "Fully Loaded",
		"still freestyle r.i.p moe 3 - Single": "still freestyle r.i.p moe 3",
		"REST IN BASS: ENCORE":                 "REST IN BASS: ENCORE",
		"Deluxe - Remastered":                  "Deluxe - Remastered",
		"Single Ladies":                        "Single Ladies",
	}
	for in, want := range cases {
		if got := stripAlbumTypeSuffix(in); got != want {
			t.Errorf("stripAlbumTypeSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestITunesAdapter_Resolve_500IsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewITunesAdapter(newTestClient(server.URL))
	art, err := adapter.Resolve(context.Background(), domain.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("Resolve on HTTP 500 = (%q, %v), want (\"\", ErrArtworkUnavailable)", art, err)
	}
}

func TestITunesAdapter_Resolve_ContextCancelledBeforeLimiterAdmitsIsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach the network once the limiter rejects a cancelled context")
	}))
	defer server.Close()

	adapter := NewITunesAdapter(newTestClient(server.URL))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	art, err := adapter.Resolve(ctx, domain.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("Resolve with a cancelled context = (%q, %v), want (\"\", ErrArtworkUnavailable)", art, err)
	}
}

func TestITunesAdapter_Resolve_RateLimitIsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	art, err := NewITunesAdapter(newTestClient(server.URL)).Resolve(context.Background(), domain.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("Resolve on HTTP 429 = (%q, %v), want (\"\", ErrArtworkUnavailable)", art, err)
	}
}

func TestITunesAdapter_Resolve_ForbiddenIsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	art, err := NewITunesAdapter(newTestClient(server.URL)).Resolve(context.Background(), domain.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("Resolve on HTTP 403 = (%q, %v), want (\"\", ErrArtworkUnavailable)", art, err)
	}
}

func TestITunesAdapter_Resolve_BadRequestIsAVerifiedMiss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	art, err := NewITunesAdapter(newTestClient(server.URL)).Resolve(context.Background(), domain.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || err != nil {
		t.Errorf("Resolve on HTTP 400 = (%q, %v), want (\"\", nil)", art, err)
	}
}

func TestITunesAdapter_Resolve_NoResultsIsAVerifiedMiss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"resultCount":0,"results":[]}`))
	}))
	defer server.Close()

	art, err := NewITunesAdapter(newTestClient(server.URL)).Resolve(context.Background(), domain.ResultKindAlbum, "Nothing", "Nobody", "")
	if art != "" || err != nil {
		t.Errorf("Resolve with no results = (%q, %v), want (\"\", nil)", art, err)
	}
}

func TestITunesAdapter_Resolve_MalformedBodyIsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[{"artworkUrl100":`))
	}))
	defer server.Close()

	art, err := NewITunesAdapter(newTestClient(server.URL)).Resolve(context.Background(), domain.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("Resolve on a truncated body = (%q, %v), want (\"\", ErrArtworkUnavailable)", art, err)
	}
}

func TestITunesAdapter_Resolve_TransportErrorIsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	serverURL := server.URL
	server.Close()

	art, err := NewITunesAdapter(newTestClient(serverURL)).Resolve(context.Background(), domain.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Errorf("Resolve against an unreachable host = (%q, %v), want (\"\", ErrArtworkUnavailable)", art, err)
	}
}

func TestITunesAdapter_Resolve_CancelledContextKeepsTheCause(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewITunesAdapter(newTestClient(server.URL)).Resolve(ctx, domain.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled kept in the chain", err)
	}
}

func TestITunesAdapter_Resolve_FailureNamesITunes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	_, err := NewITunesAdapter(newTestClient(server.URL)).Resolve(context.Background(), domain.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if err == nil || !strings.Contains(err.Error(), "itunes") {
		t.Errorf("err = %v, want a failure that names itunes", err)
	}
}
