package main

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/discovery/adapters/providers/providerhttp"
	discovery "altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"net/http"
	"testing"

	"golang.org/x/time/rate"
)

type fakeAppleArtists struct {
	artists []discovery.SearchResult
	albums  []discovery.SearchResult
	top     []discovery.SearchResult
	songs   []discovery.SearchResult
	err     error
}

func (f fakeAppleArtists) Search(_ context.Context, _ string, kinds map[discovery.ResultKind]bool) ([]discovery.SearchResult, error) {
	if kinds[discovery.ResultKindArtist] {
		return f.artists, f.err
	}
	return f.songs, f.err
}

func (f fakeAppleArtists) GetArtistAlbums(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
	return f.albums, f.err
}

func (f fakeAppleArtists) GetArtistTopTracks(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
	return f.top, f.err
}

func appleResult(kind discovery.ResultKind, id, title string) discovery.SearchResult {
	return discovery.SearchResult{Kind: kind, Title: title, Sources: []discovery.SourceRef{{Provider: discovery.ProviderAppleMusic, ExternalID: id}}}
}

func appleSong(id, album string) discovery.SearchResult {
	song := appleResult(discovery.ResultKindTrack, id, "Tom Sawyer")
	song.Subtitle, song.Album, song.Duration, song.ISRC = "Rush", album, 274, "CAR198100001"
	return song
}

func TestAppleArtistSource_KeepsOnlySongsFromTheArtistsAlbums(t *testing.T) {
	api := fakeAppleArtists{
		artists: []discovery.SearchResult{appleResult(discovery.ResultKindArtist, "r1", "Rush")},
		albums:  []discovery.SearchResult{appleResult(discovery.ResultKindAlbum, "al1", "Moving Pictures")},
		top:     []discovery.SearchResult{appleResult(discovery.ResultKindTrack, "s9", "Limelight")},
		songs:   []discovery.SearchResult{appleSong("a1", "Moving Pictures (Remastered)"), appleSong("a2", "Cover Night")},
	}
	source := appleArtistSource{api: api, limiter: rate.NewLimiter(rate.Inf, 1)}

	artists, err := source.FindArtists(t.Context(), "Rush")
	if err != nil || len(artists) != 1 || artists[0] != (providerArtist{ID: "r1", Name: "Rush"}) {
		t.Fatalf("artists = %+v, %v", artists, err)
	}
	view, err := source.Catalog(t.Context(), artists[0])
	if err != nil || len(view.Albums) != 1 || len(view.Titles) != 1 {
		t.Fatalf("view = %+v, %v", view, err)
	}
	songs, err := source.Songs(t.Context(), artists[0], view, rushTomSawyer(), domain.CleanSearchKeys("Tom Sawyer", "Rush", ""))
	if err != nil || len(songs) != 1 || songs[0].ID != "a1" || songs[0].LengthSeconds != 274 {
		t.Fatalf("songs = %+v, %v", songs, err)
	}
}

func TestAppleArtistSource_AbortsOnForbidden(t *testing.T) {
	api := fakeAppleArtists{err: providerhttp.StatusError{Status: http.StatusForbidden}}
	source := appleArtistSource{api: api, limiter: rate.NewLimiter(rate.Inf, 1)}
	if _, err := source.FindArtists(t.Context(), "Rush"); !errors.Is(err, errCatalogBlocked) {
		t.Fatalf("err = %v, want blocked", err)
	}
}
