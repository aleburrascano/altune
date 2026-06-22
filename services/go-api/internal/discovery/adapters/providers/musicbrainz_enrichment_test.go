package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
)

// mbServer stands up an httptest server that routes MB ws/2 paths to canned
// bodies, and returns an adapter pointed at it. The adapter's base URL is
// hardcoded to musicbrainz.org, so we rewrite the transport to redirect there.
func mbServer(t *testing.T, handler http.HandlerFunc) *MusicBrainzAdapter {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client := &http.Client{
		Timeout:   2 * time.Second,
		Transport: &rewriteTransport{base: srv.URL}, // shared with discogs_test.go
	}
	return NewMusicBrainzAdapter(client, "AltuneTest/1.0 ( test@altune )")
}

const kendrickArtistLookup = `{
  "genres": [
    {"name": "hip hop", "count": 15},
    {"name": "conscious hip hop", "count": 10},
    {"name": "jazz rap", "count": 9},
    {"name": "west coast hip hop", "count": 8},
    {"name": "hip hop", "count": 4},
    {"name": "", "count": 99}
  ],
  "rating": {"value": 4.3, "votes-count": 18},
  "relations": [
    {"type": "discogs", "url": {"resource": "https://www.discogs.com/artist/1539549"}},
    {"type": "wikidata", "url": {"resource": "https://www.wikidata.org/wiki/Q130798"}},
    {"type": "free streaming", "url": {"resource": "https://open.spotify.com/artist/2YZyLoL8N0Wb9xBt1NhZWg"}},
    {"type": "free streaming", "url": {"resource": "https://www.deezer.com/artist/525046"}},
    {"type": "last.fm", "url": {"resource": "https://www.last.fm/music/Kendrick+Lamar"}},
    {"type": "official homepage", "url": {"resource": "http://www.kendricklamar.com/"}}
  ]
}`

const damnReleaseGroupLookup = `{
  "genres": [
    {"name": "hip hop", "count": 14},
    {"name": "conscious hip hop", "count": 6},
    {"name": "trap", "count": 5}
  ],
  "rating": {"value": 4.1, "votes-count": 7},
  "first-release-date": "2017-04-14",
  "primary-type": "Album",
  "secondary-types": []
}`

func TestMusicBrainzAdapter_Lookup_Artist(t *testing.T) {
	a := mbServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/artist/") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if !strings.Contains(r.URL.RawQuery, "inc=genres+ratings+url-rels") {
			t.Errorf("raw query = %q, want inc=genres+ratings+url-rels (literal + separators)", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(kendrickArtistLookup))
	})

	e, err := a.Lookup(context.Background(), domain.ResultKindArtist, "381086ea-mbid")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	wantGenres := []string{"hip hop", "conscious hip hop", "jazz rap", "west coast hip hop"}
	if len(e.Genres) != len(wantGenres) {
		t.Fatalf("genres = %v, want %v (deduped, blank dropped)", e.Genres, wantGenres)
	}
	for i, g := range wantGenres {
		if e.Genres[i] != g {
			t.Errorf("genres[%d] = %q, want %q (vote-desc, dedup keeps max count)", i, e.Genres[i], g)
		}
	}
	if e.Rating != 4.3 || e.RatingVotes != 18 {
		t.Errorf("rating = %v/%d, want 4.3/18", e.Rating, e.RatingVotes)
	}
	wantIDs := map[string]string{
		"discogs":  "1539549",
		"wikidata": "Q130798",
		"spotify":  "2YZyLoL8N0Wb9xBt1NhZWg",
		"deezer":   "525046",
	}
	for k, v := range wantIDs {
		if e.ExternalIDs[k] != v {
			t.Errorf("external_ids[%q] = %q, want %q", k, e.ExternalIDs[k], v)
		}
	}
	if _, ok := e.ExternalIDs["last.fm"]; ok {
		t.Error("last.fm relation should be ignored (not in the bridge set)")
	}
	if e.Year != 0 || e.PrimaryType != "" || len(e.SecondaryTypes) != 0 {
		t.Errorf("artist DTO must zero album fields, got year=%d primary=%q secondary=%v",
			e.Year, e.PrimaryType, e.SecondaryTypes)
	}
}

func TestMusicBrainzAdapter_Lookup_ReleaseGroup(t *testing.T) {
	a := mbServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/release-group/") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if !strings.Contains(r.URL.RawQuery, "inc=genres+ratings") {
			t.Errorf("raw query = %q, want inc=genres+ratings (literal + separator)", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(damnReleaseGroupLookup))
	})

	e, err := a.Lookup(context.Background(), domain.ResultKindAlbum, "b88655ba-mbid")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if e.Year != 2017 {
		t.Errorf("year = %d, want 2017", e.Year)
	}
	if e.PrimaryType != "Album" {
		t.Errorf("primary_type = %q, want Album", e.PrimaryType)
	}
	if e.SecondaryTypes == nil || len(e.SecondaryTypes) != 0 {
		t.Errorf("secondary_types = %v, want empty non-nil", e.SecondaryTypes)
	}
	if len(e.Genres) != 3 || e.Genres[0] != "hip hop" {
		t.Errorf("genres = %v, want [hip hop, conscious hip hop, trap]", e.Genres)
	}
}

func TestMusicBrainzAdapter_Lookup_404IsError(t *testing.T) {
	a := mbServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if _, err := a.Lookup(context.Background(), domain.ResultKindAlbum, "stale-mbid"); err == nil {
		t.Error("a 404 on the lookup must return an error (service degrades it to empty)")
	}
}

func TestMusicBrainzAdapter_Lookup_UnsupportedKindEmpty(t *testing.T) {
	a := mbServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("track lookup must not hit the network in v1")
	})
	e, err := a.Lookup(context.Background(), domain.ResultKindTrack, "rec-mbid")
	if err != nil {
		t.Fatalf("Lookup(track): %v", err)
	}
	if !e.IsZero() {
		t.Errorf("track lookup must be empty in v1, got %#v", e)
	}
}

const artistSearchTwoCandidates = `{"artists": [
  {"id": "wrong-1", "name": "Humble"},
  {"id": "right-2", "name": "Kendrick Lamar"}
]}`

const rgSearchArtistMatch = `{"release-groups": [
  {"id": "rg-wrong", "title": "DAMN.", "artist-credit": [{"name": "Some Tribute Band"}]},
  {"id": "rg-right", "title": "DAMN.", "artist-credit": [{"name": "Kendrick Lamar"}]}
]}`

func TestMusicBrainzAdapter_ResolveMBID(t *testing.T) {
	tests := []struct {
		name     string
		kind     domain.ResultKind
		title    string
		subtitle string
		body     string
		want     string
	}{
		{"artist exact, skips near-miss first candidate", domain.ResultKindArtist, "Kendrick Lamar", "", artistSearchTwoCandidates, "right-2"},
		{"artist no match", domain.ResultKindArtist, "Nonexistent Artist", "", `{"artists":[{"id":"x","name":"Other"}]}`, ""},
		{"album title+artist match, filters wrong artist", domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", rgSearchArtistMatch, "rg-right"},
		{"album title match but artist mismatch", domain.ResultKindAlbum, "DAMN.", "Drake", rgSearchArtistMatch, ""},
		{"blank title", domain.ResultKindArtist, "", "", `{}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := mbServer(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			})
			got, err := a.ResolveMBID(context.Background(), tt.kind, tt.title, tt.subtitle)
			if err != nil {
				t.Fatalf("ResolveMBID: %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolveMBID = %q, want %q", got, tt.want)
			}
		})
	}
}
