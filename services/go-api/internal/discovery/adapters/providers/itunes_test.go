package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
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
	// artwork URL should be upscaled from 100x100 to 600x600
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
	// duration should be trackTimeMillis/1000 = 482
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
	// Detail-open hero artwork is requested above CAA's 1200px ceiling, not the
	// 600px search-list thumbnail — see docs/providers/itunes.md §5.2.
	if !strings.Contains(art, "1500x1500") {
		t.Errorf("hero artwork should be 1500x1500, got %q", art)
	}
}

func TestITunesAdapter_GetArtistAlbums(t *testing.T) {
	// /lookup returns the artist wrapper first, then its collections.
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
	// the artist wrapper is skipped; only the two collections map through
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
	// the album carries its own collectionId — not "0" — so detail-open content
	// lookup can key off it
	if first.Sources[0].ExternalID != "1440881047" {
		t.Errorf("externalID: got %q, want collectionId 1440881047", first.Sources[0].ExternalID)
	}
	if first.TrackCount != 15 {
		t.Errorf("TrackCount: got %d, want 15", first.TrackCount)
	}
}

func TestITunesAdapter_GetAlbumTracks(t *testing.T) {
	// /lookup returns the collection wrapper first, then its tracks.
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
	// the collection wrapper is skipped; only the two tracks map through
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

func TestITunesAdapter_LookupAlbum(t *testing.T) {
	tests := []struct {
		name         string
		albumTitle   string
		artistName   string
		profile      domain.ArtistIdentityProfile
		responseBody string
		statusCode   int
		expected     domain.AlbumVerdict
	}{
		{
			name:       "different artist name - contamination",
			albumTitle: "LOTTO DREAMS",
			artistName: "Che",
			profile: domain.ArtistIdentityProfile{
				GenreCluster:         map[string]bool{"hip-hop": true, "rap": true},
				KnownISRCRegistrants: map[string]bool{},
			},
			responseBody: `{"results":[{"collectionName":"LOTTO DREAMS","artistName":"Mr. E.L.Y","primaryGenreName":"Hip-Hop/Rap"}]}`,
			statusCode:   200,
			expected:     domain.AlbumVerdictContamination,
		},
		{
			name:       "same name incompatible genre - contamination",
			albumTitle: "Tšernobõl",
			artistName: "Che",
			profile: domain.ArtistIdentityProfile{
				GenreCluster:         map[string]bool{"hip-hop": true, "rap": true},
				KnownISRCRegistrants: map[string]bool{},
			},
			responseBody: `{"results":[{"collectionName":"Tšernobõl","artistName":"Che","primaryGenreName":"Rock"}]}`,
			statusCode:   200,
			expected:     domain.AlbumVerdictContamination,
		},
		{
			name:       "same name compatible genre - confirmed",
			albumTitle: "REST IN BASS",
			artistName: "Che",
			profile: domain.ArtistIdentityProfile{
				GenreCluster:         map[string]bool{"hip-hop": true, "rap": true},
				KnownISRCRegistrants: map[string]bool{},
			},
			responseBody: `{"results":[{"collectionName":"REST IN BASS","artistName":"Che","primaryGenreName":"Hip-Hop/Rap"}]}`,
			statusCode:   200,
			expected:     domain.AlbumVerdictConfirmed,
		},
		{
			name:       "album not found - unknown",
			albumTitle: "Nonexistent Album",
			artistName: "Che",
			profile: domain.ArtistIdentityProfile{
				GenreCluster:         map[string]bool{"hip-hop": true},
				KnownISRCRegistrants: map[string]bool{},
			},
			responseBody: `{"results":[{"collectionName":"Something Else","artistName":"Other Artist","primaryGenreName":"Pop"}]}`,
			statusCode:   200,
			expected:     domain.AlbumVerdictUnknown,
		},
		{
			name:       "api error - unknown",
			albumTitle: "Any Album",
			artistName: "Any Artist",
			profile: domain.ArtistIdentityProfile{
				GenreCluster:         map[string]bool{},
				KnownISRCRegistrants: map[string]bool{},
			},
			responseBody: "",
			statusCode:   500,
			expected:     domain.AlbumVerdictUnknown,
		},
		{
			name:       "empty genre cluster - genre check skipped - confirmed",
			albumTitle: "Some Album",
			artistName: "Che",
			profile: domain.ArtistIdentityProfile{
				GenreCluster:         map[string]bool{},
				KnownISRCRegistrants: map[string]bool{},
			},
			responseBody: `{"results":[{"collectionName":"Some Album","artistName":"Che","primaryGenreName":"Electronic"}]}`,
			statusCode:   200,
			expected:     domain.AlbumVerdictConfirmed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			adapter := NewITunesAdapter(newTestClient(server.URL))
			verdict, _, err := adapter.LookupAlbum(
				context.Background(),
				tt.albumTitle,
				tt.artistName,
				tt.profile,
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if verdict != tt.expected {
				t.Errorf("got verdict %v, want %v", verdict, tt.expected)
			}
		})
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
	// When every attempted kind fails (a single kind on HTTP 500), Search surfaces
	// an error so the circuit breaker sees the provider outage.
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
		"REST IN BASS: ENCORE":                 "REST IN BASS: ENCORE", // untouched
		"Deluxe - Remastered":                  "Deluxe - Remastered",  // not a type suffix
		"Single Ladies":                        "Single Ladies",        // not a suffix
	}
	for in, want := range cases {
		if got := stripAlbumTypeSuffix(in); got != want {
			t.Errorf("stripAlbumTypeSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}
