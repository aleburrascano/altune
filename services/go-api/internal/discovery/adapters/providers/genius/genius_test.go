package genius

import (
	"altune/go-api/internal/discovery/adapters/providers/providertest"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeniusArtworkResolver_Resolve_Song(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token-123" {
			t.Errorf("Authorization header: got %q, want %q", auth, "Bearer test-token-123")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"response": {
				"hits": [{
					"result": {
						"song_art_image_url": "https://images.genius.com/song-art.jpg",
						"header_image_url": "https://images.genius.com/header.jpg",
						"primary_artist": {
							"name": "Radiohead",
							"image_url": "https://images.genius.com/artist.jpg"
						}
					}
				}]
			}
		}`))
	}))
	defer server.Close()

	resolver := NewGeniusArtworkResolver(providertest.NewClient(server.URL), "test-token-123")
	url, err := resolver.Resolve(context.Background(), domain.ResultKindTrack, "Creep", "Radiohead", "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if url != "https://images.genius.com/song-art.jpg" {
		t.Errorf("resolve URL: got %q, want song_art_image_url", url)
	}
}

func TestGeniusArtworkResolver_Resolve_Song_FallbackToHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"response": {
				"hits": [{
					"result": {
						"song_art_image_url": "",
						"header_image_url": "https://images.genius.com/header-fallback.jpg",
						"primary_artist": {
							"name": "Radiohead",
							"image_url": "https://images.genius.com/artist.jpg"
						}
					}
				}]
			}
		}`))
	}))
	defer server.Close()

	resolver := NewGeniusArtworkResolver(providertest.NewClient(server.URL), "token")
	url, err := resolver.Resolve(context.Background(), domain.ResultKindTrack, "Creep", "Radiohead", "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if url != "https://images.genius.com/header-fallback.jpg" {
		t.Errorf("resolve URL: got %q, want header_image_url fallback", url)
	}
}

func TestGeniusArtworkResolver_Resolve_Artist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"response": {
				"hits": [{
					"result": {
						"primary_artist": {
							"name": "Radiohead",
							"image_url": "https://images.genius.com/radiohead-artist.jpg"
						}
					}
				}]
			}
		}`))
	}))
	defer server.Close()

	resolver := NewGeniusArtworkResolver(providertest.NewClient(server.URL), "token")
	url, err := resolver.Resolve(context.Background(), domain.ResultKindArtist, "Radiohead", "", "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if url != "https://images.genius.com/radiohead-artist.jpg" {
		t.Errorf("resolve URL: got %q, want artist image_url", url)
	}
}

func TestGeniusArtworkResolver_Resolve_SkipsDefaultImages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"response": {
				"hits": [{
					"result": {
						"song_art_image_url": "https://images.genius.com/default_cover.png",
						"header_image_url": "https://images.genius.com/no_image_placeholder.jpg",
						"primary_artist": {
							"name": "Test",
							"image_url": "https://images.genius.com/default_avatar.jpg"
						}
					}
				}]
			}
		}`))
	}))
	defer server.Close()

	resolver := NewGeniusArtworkResolver(providertest.NewClient(server.URL), "token")
	url, err := resolver.Resolve(context.Background(), domain.ResultKindTrack, "Something", "Test", "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL when images contain 'default'/'no_image', got %q", url)
	}
}

func TestGeniusArtworkResolver_Resolve_NoSubtitle(t *testing.T) {
	resolver := NewGeniusArtworkResolver(http.DefaultClient, "token")
	url, err := resolver.Resolve(context.Background(), domain.ResultKindTrack, "Song", "", "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL when subtitle is empty for non-artist kind, got %q", url)
	}
}

func TestGeniusArtworkResolver_Resolve_HTTPErrorIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	resolver := NewGeniusArtworkResolver(providertest.NewClient(server.URL), "token")
	url, err := resolver.Resolve(context.Background(), domain.ResultKindTrack, "Song", "Artist", "")
	if !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Fatalf("expected ErrArtworkUnavailable on HTTP 500, got: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL on HTTP 500, got %q", url)
	}
}

func TestGeniusArtworkResolver_Resolve_Artist_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	resolver := NewGeniusArtworkResolver(providertest.NewClient(server.URL), "token")
	url, err := resolver.Resolve(context.Background(), domain.ResultKindArtist, "Radiohead", "", "")
	if !errors.Is(err, ports.ErrArtworkUnavailable) {
		t.Fatalf("expected ErrArtworkUnavailable on HTTP 500, got: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL on HTTP 500, got %q", url)
	}
}

func TestGeniusArtworkResolver_Resolve_ProviderFailureIsUnavailable(t *testing.T) {
	for name, handler := range providertest.ArtworkFailureHandlers() {
		for _, kind := range []domain.ResultKind{domain.ResultKindTrack, domain.ResultKindArtist} {
			t.Run(name+" "+kind.String(), func(t *testing.T) {
				srv := httptest.NewServer(handler)
				defer srv.Close()

				resolver := NewGeniusArtworkResolver(providertest.NewClient(srv.URL), "token")
				url, err := resolver.Resolve(context.Background(), kind, "Creep", "Radiohead", "")
				if url != "" || !errors.Is(err, ports.ErrArtworkUnavailable) {
					t.Fatalf("Resolve(%s) on %s = (%q, %v), want (\"\", ErrArtworkUnavailable)", kind, name, url, err)
				}
				if !strings.Contains(err.Error(), "genius") {
					t.Errorf("err = %q, want it to name the genius source", err)
				}
			})
		}
	}
}

func TestGeniusArtworkResolver_Resolve_VerifiedMissIsEmptyWithoutError(t *testing.T) {
	handlers := providertest.ArtworkVerifiedMissHandlers()
	handlers["200 with no hits"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"hits":[]}}`))
	}
	for name, handler := range handlers {
		for _, kind := range []domain.ResultKind{domain.ResultKindTrack, domain.ResultKindArtist} {
			t.Run(name+" "+kind.String(), func(t *testing.T) {
				srv := httptest.NewServer(handler)
				defer srv.Close()

				resolver := NewGeniusArtworkResolver(providertest.NewClient(srv.URL), "token")
				url, err := resolver.Resolve(context.Background(), kind, "Creep", "Radiohead", "")
				if url != "" || err != nil {
					t.Errorf("Resolve(%s) on %s = (%q, %v), want (\"\", nil)", kind, name, url, err)
				}
			})
		}
	}
}

func TestGeniusArtworkResolver_Resolve_CancelledMidRequestIsUnavailable(t *testing.T) {
	srv, ctx := providertest.CancelMidRequestServer(t)

	resolver := NewGeniusArtworkResolver(providertest.NewClient(srv.URL), "token")
	url, err := resolver.Resolve(ctx, domain.ResultKindTrack, "Creep", "Radiohead", "")
	if url != "" || !errors.Is(err, ports.ErrArtworkUnavailable) || !errors.Is(err, context.Canceled) {
		t.Errorf("Resolve cancelled mid-request = (%q, %v), want ErrArtworkUnavailable wrapping context.Canceled", url, err)
	}
}

const artistPhoto = "https://images.genius.com/artist-photo.jpg"

func songCatalogServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestSongCatalog_ArtistSongs_KeepsPrimaryArtistSongsAndNeverUsesTheArtistPhotoAsCover(t *testing.T) {
	server := songCatalogServer(t, map[string]string{"/artists/7/songs": `{"response":{"next_page":null,"songs":[
		{"id":1,"title":"Cancun","url":"https://genius.com/c","release_date":"2022-07-24","song_art_image_url":"https://images.genius.com/cancun.jpg","header_image_url":"https://images.genius.com/cancun-h.jpg","primary_artist":{"id":7,"image_url":"` + artistPhoto + `"}},
		{"id":2,"title":"Naruto","url":"https://genius.com/n","song_art_image_url":"` + artistPhoto + `","header_image_url":"` + artistPhoto + `","primary_artist":{"id":7,"image_url":"` + artistPhoto + `"}},
		{"id":3,"title":"Header Only","url":"https://genius.com/h","song_art_image_url":"https://images.genius.com/x.jpg","header_image_url":"","primary_artist":{"id":7,"image_url":"https://images.genius.com/y.jpg","header_image_url":"https://images.genius.com/x.jpg"}},
		{"id":4,"title":"Feature","url":"https://genius.com/f","song_art_image_url":"https://images.genius.com/f.jpg","primary_artist":{"id":99}}
	]}}`})
	catalog := NewSongCatalog(providertest.NewClient(server.URL), "tok")

	songs, err := catalog.ArtistSongs(context.Background(), 7, 3)
	if err != nil {
		t.Fatalf("ArtistSongs: %v", err)
	}

	if len(songs) != 3 {
		t.Fatalf("got %d songs, want 3 (the feature is dropped): %+v", len(songs), songs)
	}
	if songs[0].CoverURL != "https://images.genius.com/cancun.jpg" || songs[0].ReleaseDate != "2022-07-24" {
		t.Errorf("cancun = %+v, want its own cover and release date", songs[0])
	}
	if songs[1].CoverURL != "" {
		t.Errorf("naruto cover = %q, want none (it is the artist photo)", songs[1].CoverURL)
	}
	if songs[2].CoverURL != "" {
		t.Errorf("header-only cover = %q, want none (it is the artist header)", songs[2].CoverURL)
	}
}

func TestSongCatalog_ArtistSongs_StopsAtThePageCap(t *testing.T) {
	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		fmt.Fprintf(w, `{"response":{"next_page":%d,"songs":[]}}`, pages+1)
	}))
	defer server.Close()
	catalog := NewSongCatalog(providertest.NewClient(server.URL), "tok")

	if _, err := catalog.ArtistSongs(context.Background(), 7, 2); err != nil {
		t.Fatalf("ArtistSongs: %v", err)
	}
	if pages != 2 {
		t.Errorf("fetched %d pages, want 2", pages)
	}
}

func TestSongCatalog_FindArtistID_RequiresAnExactPrimaryArtistName(t *testing.T) {
	server := songCatalogServer(t, map[string]string{"/search": `{"response":{"hits":[
		{"result":{"primary_artist":{"id":11,"name":"Internet Money"}}},
		{"result":{"primary_artist":{"id":22,"name":"Don Toliver Jr"}}},
		{"result":{"primary_artist":{"id":33,"name":"don toliver"}}}
	]}}`})
	catalog := NewSongCatalog(providertest.NewClient(server.URL), "tok")

	id, found, err := catalog.FindArtistID(context.Background(), "Don Toliver", nil)

	if err != nil || !found || id != 33 {
		t.Errorf("FindArtistID = (%d, %v, %v), want (33, true, nil)", id, found, err)
	}
}

func TestSongCatalog_FindArtistID_NoMatchIsNotFound(t *testing.T) {
	server := songCatalogServer(t, map[string]string{"/search": `{"response":{"hits":[{"result":{"primary_artist":{"id":11,"name":"Internet Money"}}}]}}`})
	catalog := NewSongCatalog(providertest.NewClient(server.URL), "tok")

	_, found, err := catalog.FindArtistID(context.Background(), "Don Toliver", []string{"Magnolia"})

	if err != nil || found {
		t.Errorf("FindArtistID = (found %v, err %v), want not found without error", found, err)
	}
}

func TestSongCatalog_HasMedia_OnlyYouTubeAndSoundCloudCount(t *testing.T) {
	server := songCatalogServer(t, map[string]string{
		"/songs/1": `{"response":{"song":{"media":[{"provider":"spotify"},{"provider":"youtube"}]}}}`,
		"/songs/2": `{"response":{"song":{"media":[{"provider":"spotify"}]}}}`,
		"/songs/3": `{"response":{"song":{"media":[]}}}`,
	})
	catalog := NewSongCatalog(providertest.NewClient(server.URL), "tok")

	for id, want := range map[int]bool{1: true, 2: false, 3: false} {
		got, err := catalog.HasMedia(context.Background(), id)
		if err != nil || got != want {
			t.Errorf("HasMedia(%d) = (%v, %v), want %v", id, got, err, want)
		}
	}
}

func TestArtworkSourceTags(t *testing.T) {
	tests := []struct {
		got  domain.ProviderKey
		want string
	}{
		{(&GeniusArtworkResolver{}).ArtworkSource(), "genius"},
	}
	for _, tt := range tests {
		if tt.got.String() != tt.want {
			t.Errorf("ArtworkSource = %q, want %q", tt.got, tt.want)
		}
	}
}

func TestGeniusArtworkResolver_Resolve_MalformedHitFailsTheResponse(t *testing.T) {
	for name, body := range map[string]string{
		"non-object result":         `{"response": {"hits": [{"result": "oops"}, {"result": {"song_art_image_url": "https://images.genius.com/ok.jpg"}}]}}`,
		"non-object primary_artist": `{"response": {"hits": [{"result": {"primary_artist": 3}}, {"result": {"song_art_image_url": "https://images.genius.com/ok.jpg"}}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Write([]byte(body))
			}))
			defer server.Close()

			resolver := NewGeniusArtworkResolver(providertest.NewClient(server.URL), "t")
			got, err := resolver.Resolve(context.Background(), domain.ResultKindTrack, "Creep", "Radiohead", "")
			if err == nil {
				t.Fatalf("Resolve: got %q and no error, want an error", got)
			}
			if got != "" {
				t.Errorf("resolve URL: got %q, want empty", got)
			}
		})
	}
}

func TestSongCatalog_SongDetail_ReadsStreamingLinksAndAlbum(t *testing.T) {
	server := songCatalogServer(t, map[string]string{
		"/songs/1": `{"response":{"song":{"album":{"name":"Heaven or Hell"},"media":[{"provider":"spotify"},{"provider":"youtube"}]}}}`,
		"/songs/2": `{"response":{"song":{"album":null,"apple_music_id":"1440","media":[]}}}`,
		"/songs/3": `{"response":{"song":{"apple_music_id":"","media":[{"provider":"soundcloud"},{"provider":"apple_music"}]}}}`,
	})
	catalog := NewSongCatalog(providertest.NewClient(server.URL), "tok")

	want := map[int]domain.GeniusSongDetail{
		1: {HasMedia: true, OnStreamingSite: true, AlbumTitle: "Heaven or Hell"},
		2: {OnStreamingSite: true},
		3: {HasMedia: true},
	}
	for id, w := range want {
		got, err := catalog.SongDetail(context.Background(), id)
		if err != nil || got != w {
			t.Errorf("SongDetail(%d) = (%+v, %v), want %+v", id, got, err, w)
		}
	}
}
