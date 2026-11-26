package itunes_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/providers/providertest"
	"altune/go-api/internal/shared/queuetimeout"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"
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

	adapter := search.NewITunesAdapter(providertest.NewClient(server.URL))
	results, err := adapter.Search(context.Background(), "stairway to heaven", map[search.ResultKind]bool{
		search.ResultKindTrack: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != search.ResultKindTrack {
		t.Errorf("kind: got %v, want %v", r.Kind, search.ResultKindTrack)
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
	if r.Confidence != search.ConfidenceLow {
		t.Errorf("confidence: got %v, want %v", r.Confidence, search.ConfidenceLow)
	}
	if len(r.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(r.Sources))
	}
	if r.Sources[0].Provider != search.ProviderITunes {
		t.Errorf("source provider: got %v, want %v", r.Sources[0].Provider, search.ProviderITunes)
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
	if r.Duration != 482 {
		t.Errorf("Duration: got %d, want 482", r.Duration)
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

	adapter := search.NewITunesAdapter(providertest.NewClient(server.URL))
	results, err := adapter.Search(context.Background(), "pink floyd", map[search.ResultKind]bool{
		search.ResultKindArtist: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != search.ResultKindArtist {
		t.Errorf("kind: got %v, want %v", r.Kind, search.ResultKindArtist)
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

	adapter := search.NewITunesAdapter(providertest.NewClient(server.URL))
	art, err := adapter.Resolve(context.Background(), search.ResultKindAlbum, "Discovery", "Daft Punk", "")
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

	adapter := search.NewITunesAdapter(providertest.NewClient(server.URL))
	results, err := adapter.GetArtistAlbums(context.Background(), search.ProviderITunes, "368183298")
	if err != nil {
		t.Fatalf("GetArtistAlbums: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 albums, got %d", len(results))
	}
	first := results[0]
	if first.Kind != search.ResultKindAlbum {
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

	adapter := search.NewITunesAdapter(providertest.NewClient(server.URL))
	results, err := adapter.GetAlbumTracks(context.Background(), search.ProviderITunes, "1440881722")
	if err != nil {
		t.Fatalf("GetAlbumTracks: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(results))
	}
	if results[0].Kind != search.ResultKindTrack {
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

	adapter := search.NewITunesAdapter(providertest.NewClient(server.URL))
	results, err := adapter.Search(context.Background(), "anything", map[search.ResultKind]bool{
		search.ResultKindTrack: true,
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
		if got := search.ITunesStripAlbumTypeSuffix(in); got != want {
			t.Errorf("stripAlbumTypeSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestITunesAdapter_Resolve_500IsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := search.NewITunesAdapter(providertest.NewClient(server.URL))
	art, err := adapter.Resolve(context.Background(), search.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, search.ErrArtworkUnavailable) {
		t.Errorf("Resolve on HTTP 500 = (%q, %v), want (\"\", ErrArtworkUnavailable)", art, err)
	}
}

func TestITunesAdapter_Resolve_ContextCancelledBeforeTheRequestIsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach the network for a cancelled context")
	}))
	defer server.Close()

	adapter := search.NewITunesAdapter(providertest.NewClient(server.URL))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	art, err := adapter.Resolve(ctx, search.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, search.ErrArtworkUnavailable) {
		t.Errorf("Resolve with a cancelled context = (%q, %v), want (\"\", ErrArtworkUnavailable)", art, err)
	}
}

func TestITunesAdapter_Resolve_RateLimitIsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	art, err := search.NewITunesAdapter(providertest.NewClient(server.URL)).Resolve(context.Background(), search.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, search.ErrArtworkUnavailable) {
		t.Errorf("Resolve on HTTP 429 = (%q, %v), want (\"\", ErrArtworkUnavailable)", art, err)
	}
}

func TestITunesAdapter_Resolve_ForbiddenIsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	art, err := search.NewITunesAdapter(providertest.NewClient(server.URL)).Resolve(context.Background(), search.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, search.ErrArtworkUnavailable) {
		t.Errorf("Resolve on HTTP 403 = (%q, %v), want (\"\", ErrArtworkUnavailable)", art, err)
	}
}

func TestITunesAdapter_Resolve_BadRequestIsAVerifiedMiss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	art, err := search.NewITunesAdapter(providertest.NewClient(server.URL)).Resolve(context.Background(), search.ResultKindAlbum, "Discovery", "Daft Punk", "")
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

	art, err := search.NewITunesAdapter(providertest.NewClient(server.URL)).Resolve(context.Background(), search.ResultKindAlbum, "Nothing", "Nobody", "")
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

	art, err := search.NewITunesAdapter(providertest.NewClient(server.URL)).Resolve(context.Background(), search.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, search.ErrArtworkUnavailable) {
		t.Errorf("Resolve on a truncated body = (%q, %v), want (\"\", ErrArtworkUnavailable)", art, err)
	}
}

func TestITunesAdapter_Resolve_TransportErrorIsArtworkUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	serverURL := server.URL
	server.Close()

	art, err := search.NewITunesAdapter(providertest.NewClient(serverURL)).Resolve(context.Background(), search.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if art != "" || !errors.Is(err, search.ErrArtworkUnavailable) {
		t.Errorf("Resolve against an unreachable host = (%q, %v), want (\"\", ErrArtworkUnavailable)", art, err)
	}
}

func TestITunesAdapter_Resolve_CancelledContextKeepsTheCause(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := search.NewITunesAdapter(providertest.NewClient(server.URL)).Resolve(ctx, search.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled kept in the chain", err)
	}
}

func TestITunesAdapter_Resolve_FailureNamesITunes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	_, err := search.NewITunesAdapter(providertest.NewClient(server.URL)).Resolve(context.Background(), search.ResultKindAlbum, "Discovery", "Daft Punk", "")
	if err == nil || !strings.Contains(err.Error(), "itunes") {
		t.Errorf("err = %v, want a failure that names itunes", err)
	}
}

func TestAdapterNames(t *testing.T) {
	tests := []struct {
		got, want search.ProviderName
	}{
		{(&search.ITunesAdapter{}).Name(), search.ProviderITunes},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("Name() = %v, want %v", tt.got, tt.want)
		}
	}
}

func TestAdapterSearchTimeouts(t *testing.T) {
	tests := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"itunes", (&search.ITunesAdapter{}).SearchTimeout(), 4 * time.Second},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s SearchTimeout = %v, want %v", tt.name, tt.got, tt.want)
		}
	}
}

func TestArtworkSourceTags(t *testing.T) {
	tests := []struct {
		got  search.ProviderKey
		want string
	}{
		{(&search.ITunesAdapter{}).ArtworkSource(), "itunes"},
	}
	for _, tt := range tests {
		if tt.got.String() != tt.want {
			t.Errorf("ArtworkSource = %q, want %q", tt.got, tt.want)
		}
	}
}

func albumTracksLookup(client *http.Client) func(context.Context) error {
	adapter := search.NewITunesAdapter(client)
	return func(ctx context.Context) error {
		_, err := adapter.GetAlbumTracks(ctx, search.ProviderITunes, "1")
		return err
	}
}

func TestITunesAdapter_PacesNothingItself(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		lookup := albumTracksLookup(&http.Client{Transport: providertest.FakeRoundTripper{Fn: func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"results":[]}`)),
				Request:    r,
			}, nil
		}}})
		start := time.Now()

		for i := range 10 {
			if err := lookup(context.Background()); err != nil {
				t.Fatalf("lookup %d: %v", i+1, err)
			}
		}

		if elapsed := time.Since(start); elapsed != 0 {
			t.Fatalf("ten back-to-back lookups took %v, want no wait of the adapter's own because the transport paces the host", elapsed)
		}
	})
}

func TestITunesAdapter_PassesATransportQueueTimeoutThroughClassified(t *testing.T) {
	shed := fmt.Errorf("%w: %w", queuetimeout.Err, context.DeadlineExceeded)
	lookup := albumTracksLookup(&http.Client{Transport: providertest.FakeRoundTripper{Fn: func(*http.Request) (*http.Response, error) {
		return nil, shed
	}}})

	err := lookup(context.Background())

	if !errors.Is(err, search.ErrProviderRateLimitQueueTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the shed to stay a queue timeout and a DeadlineExceeded", err)
	}
}
