package spotify

import (
	"altune/go-api/internal/discovery/adapters/providers/providertest"
	"altune/go-api/internal/discovery/domain"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func newContentSpotifyAdapter(srv *httptest.Server) *SpotifyAdapter {
	a := NewSpotifyAdapter(srv.Client())
	a.pathfinderURL = srv.URL
	a.resolver.Cached = &spotifySession{
		accessToken:  "test-access-token",
		accessExpiry: providertest.FarFuture,
		clientToken:  "test-client-token",
		clientExpiry: providertest.FarFuture,
	}
	return a
}

func operationNameOf(t *testing.T, r *http.Request) string {
	t.Helper()
	raw, _ := io.ReadAll(r.Body)
	var req struct {
		OperationName string `json:"operationName"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return req.OperationName
}

func TestSpotifyAdapter_GetArtistAlbums(t *testing.T) {
	const discographyJSON = `{"data":{"artistUnion":{"discography":{"all":{"items":[
		{"releases":{"items":[
			{"id":"al1","name":"After Hours","type":"ALBUM","coverArt":{"sources":[{"url":"https://i/300.jpg","width":300},{"url":"https://i/640.jpg","width":640}]},"date":{"isoString":"2020-03-20T00:00:00Z","year":2020},"tracks":{"totalCount":14},"sharingInfo":{"shareUrl":"https://open.spotify.com/album/al1?si=abc"}},
			{"id":"al1b","name":"After Hours (Deluxe)","type":"ALBUM","date":{"isoString":"2020-03-21T00:00:00Z","year":2020},"tracks":{"totalCount":16}}
		]}},
		{"releases":{"items":[
			{"id":"s1","name":"Live Single","type":"SINGLE","coverArt":{"sources":[{"url":"https://i/x.jpg","width":640}]},"date":{"isoString":"2021-01-05T00:00:00Z","year":2021},"tracks":{"totalCount":1}}
		]}}
	]}}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-access-token" {
			t.Errorf("Authorization = %q, want Bearer test-access-token", got)
		}
		if op := operationNameOf(t, r); op != "queryArtistDiscographyAll" {
			t.Errorf("operationName = %q, want queryArtistDiscographyAll", op)
		}
		_, _ = w.Write([]byte(discographyJSON))
	}))
	defer srv.Close()

	a := newContentSpotifyAdapter(srv)
	albums, err := a.GetArtistAlbums(t.Context(), domain.ProviderSpotify, "artist-1")
	if err != nil {
		t.Fatalf("GetArtistAlbums error = %v", err)
	}
	if len(albums) != 2 {
		t.Fatalf("albums = %d, want 2 (one representative per group)", len(albums))
	}
	if albums[0].Title != "After Hours" || albums[0].ReleaseDate != "2020-03-20" || albums[0].TrackCount != 14 {
		t.Errorf("album[0] = %+v", albums[0])
	}
	if albums[0].ImageURL != "https://i/640.jpg" {
		t.Errorf("album[0].ImageURL = %q, want the widest image", albums[0].ImageURL)
	}
	if albums[0].Sources[0].URL != "https://open.spotify.com/album/al1" {
		t.Errorf("album[0] URL = %q, want the share URL stripped of ?si=", albums[0].Sources[0].URL)
	}
	if albums[1].RecordType != "single" {
		t.Errorf("album[1] record_type = %v, want single", albums[1].RecordType)
	}
}

func TestSpotifyAdapter_GetArtistTopTracks(t *testing.T) {
	const overviewJSON = `{"data":{"artistUnion":{"discography":{"topTracks":{"items":[
		{"track":{"id":"t1","name":"Blinding Lights","contentRating":{"label":"EXPLICIT"},"duration":{"totalMilliseconds":200000},"albumOfTrack":{"coverArt":{"sources":[{"url":"https://i/640.jpg","width":640}]}},"artists":{"items":[{"profile":{"name":"The Weeknd"}}]}}}
	]}}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if op := operationNameOf(t, r); op != "queryArtistOverview" {
			t.Errorf("operationName = %q, want queryArtistOverview", op)
		}
		_, _ = w.Write([]byte(overviewJSON))
	}))
	defer srv.Close()

	a := newContentSpotifyAdapter(srv)
	tracks, err := a.GetArtistTopTracks(t.Context(), domain.ProviderSpotify, "artist-1")
	if err != nil {
		t.Fatalf("GetArtistTopTracks error = %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("tracks = %d, want 1", len(tracks))
	}
	tr := tracks[0]
	if tr.Title != "Blinding Lights" || tr.Subtitle != "The Weeknd" || tr.Duration != 200 {
		t.Errorf("track = %+v", tr)
	}
	if tr.Explicit == nil || !*tr.Explicit {
		t.Errorf("track explicit = %v, want true", tr.Explicit)
	}
}

func TestSpotifyAdapter_GetAlbumTracks(t *testing.T) {
	const tracksJSON = `{"data":{"albumUnion":{"tracksV2":{"items":[
		{"track":{"uri":"spotify:track:tr1","name":"Like Lil Mexico","trackNumber":1,"contentRating":{"label":"EXPLICIT"},"duration":{"totalMilliseconds":176830},"artists":{"items":[{"profile":{"name":"Che"}}]}}},
		{"track":{"uri":"spotify:track:tr2","name":"Second","trackNumber":2,"duration":{"totalMilliseconds":120000},"artists":{"items":[{"profile":{"name":"Che"}}]}}}
	]}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if op := operationNameOf(t, r); op != "queryAlbumTracks" {
			t.Errorf("operationName = %q, want queryAlbumTracks", op)
		}
		_, _ = w.Write([]byte(tracksJSON))
	}))
	defer srv.Close()

	a := newContentSpotifyAdapter(srv)
	tracks, err := a.GetAlbumTracks(t.Context(), domain.ProviderSpotify, "album-1")
	if err != nil {
		t.Fatalf("GetAlbumTracks error = %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("tracks = %d, want 2 (album order preserved)", len(tracks))
	}
	if tracks[0].Title != "Like Lil Mexico" || tracks[0].Subtitle != "Che" || tracks[0].Duration != 176 {
		t.Errorf("track[0] = %+v", tracks[0])
	}
	if tracks[0].Sources[0].ExternalID != "tr1" {
		t.Errorf("track[0] id = %q, want tr1 (extracted from uri)", tracks[0].Sources[0].ExternalID)
	}
	if tracks[0].Explicit == nil || !*tracks[0].Explicit {
		t.Errorf("track[0] explicit = %v, want true", tracks[0].Explicit)
	}
}

func TestSpotifyAdapter_GetAlbumTracks_namesEveryOtherArtistAsFeatured(t *testing.T) {
	const tracksJSON = `{"data":{"albumUnion":{"tracksV2":{"items":[
		{"track":{"uri":"spotify:track:tr1","name":"Rich Flex","artists":{"items":[{"profile":{"name":"Drake"}},{"profile":{"name":"21 Savage"}}]}}},
		{"track":{"uri":"spotify:track:tr2","name":"Solo","artists":{"items":[{"profile":{"name":"Drake"}}]}}}
	]}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(tracksJSON))
	}))
	defer srv.Close()

	tracks, err := newContentSpotifyAdapter(srv).GetAlbumTracks(t.Context(), domain.ProviderSpotify, "album-1")
	if err != nil {
		t.Fatal(err)
	}

	feats := domain.FeaturedArtistsFromExtras(tracks[0].Extras)
	if len(feats) != 1 || feats[0].Name != "21 Savage" || feats[0].Role != domain.RoleFeatured {
		t.Errorf("Rich Flex featured = %+v, want 21 Savage", feats)
	}
	if _, present := tracks[1].Extras[domain.ExtraFeaturedArtists]; present {
		t.Errorf("Solo has featured artists %v, want none", tracks[1].Extras[domain.ExtraFeaturedArtists])
	}
}

func TestSpotifyAdapter_Content_surfacesGraphQLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"PersistedQueryNotFound"}],"data":null}`))
	}))
	defer srv.Close()

	a := newContentSpotifyAdapter(srv)
	_, err := a.GetArtistAlbums(t.Context(), domain.ProviderSpotify, "artist-1")
	if err == nil || !strings.Contains(err.Error(), "PersistedQueryNotFound") {
		t.Fatalf("err = %v, want a surfaced GraphQL error", err)
	}
}

func TestSpotifyAdapter_doPathfinderContent_non200ErrorTextAndStatusPinned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"errors":[{"message":"must not be surfaced"}]}`))
	}))
	defer srv.Close()

	a := newContentSpotifyAdapter(srv)
	var out any
	status, err := a.doPathfinderContent(t.Context(), a.resolver.Cached, "queryArtistDiscographyAll", "hash", map[string]any{}, &out)
	if err == nil || err.Error() != "http status 429" {
		t.Fatalf("err = %v, want exactly %q (status gate runs before the GraphQL envelope)", err, "http status 429")
	}
	if status != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429 surfaced for auth-retry branching", status)
	}
}

func offsetOf(t *testing.T, raw []byte) int {
	t.Helper()
	var req struct {
		Variables struct {
			Offset int `json:"offset"`
		} `json:"variables"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return req.Variables.Offset
}

func TestSpotifyAdapter_GetArtistAlbums_paginates(t *testing.T) {
	const page1 = `{"data":{"artistUnion":{"discography":{"all":{"totalCount":3,"items":[
		{"releases":{"items":[{"id":"al1","name":"First","type":"ALBUM"}]}},
		{"releases":{"items":[{"id":"al2","name":"Second","type":"ALBUM"}]}}
	]}}}}}`
	const page2 = `{"data":{"artistUnion":{"discography":{"all":{"totalCount":3,"items":[
		{"releases":{"items":[{"id":"al3","name":"Third","type":"SINGLE"}]}}
	]}}}}}`
	var offsets []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		offset := offsetOf(t, raw)
		offsets = append(offsets, offset)
		if offset == 0 {
			_, _ = w.Write([]byte(page1))
			return
		}
		_, _ = w.Write([]byte(page2))
	}))
	defer srv.Close()

	a := newContentSpotifyAdapter(srv)
	albums, err := a.GetArtistAlbums(t.Context(), domain.ProviderSpotify, "artist-1")
	if err != nil {
		t.Fatalf("GetArtistAlbums error = %v", err)
	}
	if len(offsets) != 2 || offsets[0] != 0 || offsets[1] != 50 {
		t.Errorf("offsets = %v, want [0 50]", offsets)
	}
	if len(albums) != 3 {
		t.Fatalf("albums = %d, want 3 across pages", len(albums))
	}
	for i, want := range []string{"First", "Second", "Third"} {
		if albums[i].Title != want {
			t.Errorf("album[%d] = %q, want %q (pages appended in request order)", i, albums[i].Title, want)
		}
	}
}

func TestSpotifyAdapter_GetAlbumTracks_paginates(t *testing.T) {
	const page1 = `{"data":{"albumUnion":{"tracksV2":{"totalCount":3,"items":[
		{"track":{"uri":"spotify:track:tr1","name":"One","trackNumber":1}},
		{"track":{"uri":"spotify:track:tr2","name":"Two","trackNumber":2}}
	]}}}}`
	const page2 = `{"data":{"albumUnion":{"tracksV2":{"totalCount":3,"items":[
		{"track":{"uri":"spotify:track:tr3","name":"Three","trackNumber":3}}
	]}}}}`
	var offsets []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		offset := offsetOf(t, raw)
		offsets = append(offsets, offset)
		if offset == 0 {
			_, _ = w.Write([]byte(page1))
			return
		}
		_, _ = w.Write([]byte(page2))
	}))
	defer srv.Close()

	a := newContentSpotifyAdapter(srv)
	tracks, err := a.GetAlbumTracks(t.Context(), domain.ProviderSpotify, "album-1")
	if err != nil {
		t.Fatalf("GetAlbumTracks error = %v", err)
	}
	if len(offsets) != 2 || offsets[0] != 0 || offsets[1] != 50 {
		t.Errorf("offsets = %v, want [0 50]", offsets)
	}
	if len(tracks) != 3 {
		t.Fatalf("tracks = %d, want 3 across pages", len(tracks))
	}
	for i, want := range []string{"One", "Two", "Three"} {
		if tracks[i].Title != want {
			t.Errorf("track[%d] = %q, want %q (album order preserved across pages)", i, tracks[i].Title, want)
		}
	}
}

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
	var partial *domain.PartialResultError
	if !errors.As(err, &partial) || partial.Page != 1 {
		t.Fatalf("expected a PartialResultError for page 1 alongside the earlier pages, got: %v", err)
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
	var partial *domain.PartialResultError
	if !errors.As(err, &partial) || partial.Page != 1 {
		t.Fatalf("expected a PartialResultError for page 1 alongside the earlier pages, got: %v", err)
	}
	if len(tracks) != 2 || tracks[0].Title != "One" || tracks[1].Title != "Two" {
		t.Fatalf("tracks = %+v, want the 2 page-1 tracks kept", tracks)
	}
}

func TestSpotifyAdapter_GetArtistAlbums_reResolvesSessionOnAuthFailure(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fresh-access-token" {
			t.Errorf("Authorization on retry = %q, want the freshly re-resolved token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": {"artistUnion": {"discography": {"all": {"items": [
			{"releases": {"items": [{
				"id": "alb-1", "name": "After Hours", "type": "ALBUM",
				"date": {"isoString": "2020-03-20T00:00:00Z", "year": 2020},
				"tracks": {"totalCount": 14}
			}]}}
		]}}}}}`))
	}))
	defer srv.Close()

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/server-time":
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": 1700000000})
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"accessToken":                      "fresh-access-token",
				"accessTokenExpirationTimestampMs": 99999999999999,
			})
		case "/clienttoken":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"granted_token": map[string]any{"token": "fresh-client-token", "expires_after_seconds": 999999},
			})
		}
	}))
	defer tokenSrv.Close()

	a := newTestSpotifyAdapter(srv)
	a.resolver.serverTimeURL = tokenSrv.URL + "/server-time"
	a.resolver.accessTokenURL = tokenSrv.URL + "/token"
	a.resolver.clientTokenURL = tokenSrv.URL + "/clienttoken"

	albums, err := a.GetArtistAlbums(t.Context(), domain.ProviderSpotify, "artist-1")
	if err != nil {
		t.Fatalf("GetArtistAlbums: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (initial 401 then retry after re-resolve)", calls)
	}
	if len(albums) != 1 || albums[0].Title != "After Hours" {
		t.Errorf("albums = %+v", albums)
	}
}

func TestMapSpotifyAlbumTrack_CarriesTheProvidersTrackAndDiscNumber(t *testing.T) {
	r, ok := mapSpotifyAlbumTrack(spotifyAlbumTrack{Name: "Bonus One", URI: "spotify:track:abc", TrackNumber: 1, DiscNumber: 2})
	if !ok {
		t.Fatal("track dropped")
	}
	if r.TrackPosition != (domain.TrackPosition{Disc: 2, Track: 1}) {
		t.Errorf("track position = %+v, want track 1 disc 2", r.TrackPosition)
	}
}

func servePayload(t *testing.T, fixture string) *httptest.Server {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(raw)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGetAlbumTracks_PayloadFillsPositionExplicitAndContributors(t *testing.T) {
	adapter := newContentSpotifyAdapter(servePayload(t, "payload_album_tracks_with_featured_artist.json"))

	results, err := adapter.GetAlbumTracks(t.Context(), domain.ProviderSpotify, "41GuZcammIkupMPKH2OJ6I")
	if err != nil || len(results) != 2 {
		t.Fatalf("GetAlbumTracks = %d results, err %v, want 2", len(results), err)
	}

	sicko, stargazing := results[0], results[1]
	checks := []struct {
		field     string
		got, want any
	}{
		{"sicko position", sicko.TrackPosition, domain.TrackPosition{Disc: 1, Track: 3}},
		{"sicko explicit", sicko.Explicit != nil && *sicko.Explicit, true},
		{"sicko duration", sicko.Duration, 312},
		{"sicko contributors", sicko.Contributors, []domain.Contributor{
			{ProviderID: "0Y5tJX1MQlPlqiwlOH1tJY", Name: "Travis Scott", Role: domain.ContributorMain},
			{ProviderID: "3TVXtAsR1Inumwj472S9r4", Name: "Drake", Role: domain.ContributorFeatured},
		}},
		{"stargazing position", stargazing.TrackPosition, domain.TrackPosition{Disc: 2, Track: 1}},
		{"stargazing explicit", stargazing.Explicit, (*bool)(nil)},
		{"stargazing contributors", stargazing.Contributors, []domain.Contributor{
			{ProviderID: "0Y5tJX1MQlPlqiwlOH1tJY", Name: "Travis Scott", Role: domain.ContributorMain},
		}},
		{"sicko extras track_number", sicko.Extras["track_number"], nil},
		{"sicko extras explicit", sicko.Extras["explicit"], nil},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s = %#v, want %#v", c.field, c.got, c.want)
		}
	}
}

func TestSearch_PayloadFillsTrackAndAlbumContract(t *testing.T) {
	adapter := newTestSpotifyAdapter(servePayload(t, "payload_search_track_and_album.json"))

	results, err := adapter.Search(t.Context(), "sicko mode", map[domain.ResultKind]bool{domain.ResultKindTrack: true, domain.ResultKindAlbum: true})
	if err != nil || len(results) != 2 {
		t.Fatalf("Search = %d results, err %v, want a track and an album", len(results), err)
	}

	var track, album domain.SearchResult
	for _, r := range results {
		if r.Kind == domain.ResultKindTrack {
			track = r
		} else {
			album = r
		}
	}
	checks := []struct {
		field     string
		got, want any
	}{
		{"track explicit", track.Explicit != nil && *track.Explicit, true},
		{"track album", track.Album, "ASTROWORLD"},
		{"track contributors", track.Contributors, []domain.Contributor{
			{ProviderID: "0Y5tJX1MQlPlqiwlOH1tJY", Name: "Travis Scott", Role: domain.ContributorMain},
			{ProviderID: "3TVXtAsR1Inumwj472S9r4", Name: "Drake", Role: domain.ContributorFeatured},
		}},
		{"track isrc", track.ISRC, ""},
		{"album record type", album.RecordType, domain.RecordTypeAlbum},
		{"album year", album.Year, 2018},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s = %#v, want %#v", c.field, c.got, c.want)
		}
	}
}
