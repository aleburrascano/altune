package providers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// A later-page failure keeps the pages already fetched — depth is best-effort,
// presence is not (the SoundCloud doSearch policy, applied to pathfinder).

func TestSpotifyAdapter_GetArtistAlbums_laterPageErrorKeepsEarlierPages(t *testing.T) {
	const page1 = `{"data":{"artistUnion":{"discography":{"all":{"totalCount":3,"items":[
		{"releases":{"items":[{"id":"al1","name":"First","type":"ALBUM"}]}},
		{"releases":{"items":[{"id":"al2","name":"Second","type":"ALBUM"}]}}
	]}}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if offsetOf(t, raw) != 0 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(page1))
	}))
	defer srv.Close()

	a := newContentSpotifyAdapter(srv)
	albums, err := a.GetArtistAlbums(t.Context(), domain.ProviderSpotify, "artist-1")
	if err != nil {
		t.Fatalf("expected the partial set on a later-page failure, got error: %v", err)
	}
	if len(albums) != 2 || albums[0].Title != "First" || albums[1].Title != "Second" {
		t.Fatalf("albums = %+v, want the 2 page-1 albums kept", albums)
	}
}

func TestSpotifyAdapter_GetAlbumTracks_laterPageErrorKeepsEarlierPages(t *testing.T) {
	const page1 = `{"data":{"albumUnion":{"tracksV2":{"totalCount":3,"items":[
		{"track":{"uri":"spotify:track:tr1","name":"One","trackNumber":1}},
		{"track":{"uri":"spotify:track:tr2","name":"Two","trackNumber":2}}
	]}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if offsetOf(t, raw) != 0 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(page1))
	}))
	defer srv.Close()

	a := newContentSpotifyAdapter(srv)
	tracks, err := a.GetAlbumTracks(t.Context(), domain.ProviderSpotify, "album-1")
	if err != nil {
		t.Fatalf("expected the partial set on a later-page failure, got error: %v", err)
	}
	if len(tracks) != 2 || tracks[0].Title != "One" || tracks[1].Title != "Two" {
		t.Fatalf("tracks = %+v, want the 2 page-1 tracks kept", tracks)
	}
}
