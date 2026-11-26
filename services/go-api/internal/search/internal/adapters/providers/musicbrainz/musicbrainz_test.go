package musicbrainz_test

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/adapters/providers/musicbrainz"
	"altune/go-api/internal/search/internal/adapters/providers/providerrequests"
	"altune/go-api/internal/search/internal/adapters/providers/providertest"
	"altune/go-api/internal/search/internal/core/ports"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func mbReleaseGroupJSON(rgID, title, artistName, artistMBID string) string {
	return `{
		"id": "` + rgID + `",
		"title": "` + title + `",
		"artist-credit": [{"name": "` + artistName + `", "artist": {"id": "` + artistMBID + `", "name": "` + artistName + `"}}]
	}`
}

func TestMusicBrainzAdapter_SearchStructured_Recordings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ws/2/recording") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"recordings": [{
				"id": "abc-123-def",
				"title": "Paranoid Android",
				"isrcs": ["GBAYE9700011"],
				"artist-credit": [{"name": "Radiohead"}]
			}]
		}`))
	}))
	defer server.Close()

	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	results, err := adapter.SearchStructured(context.Background(), "paranoid android", "", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindTrack {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindTrack)
	}
	if r.Title != "Paranoid Android" {
		t.Errorf("title: got %q, want %q", r.Title, "Paranoid Android")
	}
	if r.Subtitle != "Radiohead" {
		t.Errorf("subtitle: got %q, want %q", r.Subtitle, "Radiohead")
	}
	if r.Confidence != domain.ConfidenceLow {
		t.Errorf("confidence: got %v, want %v", r.Confidence, domain.ConfidenceLow)
	}
	if len(r.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(r.Sources))
	}
	if r.Sources[0].Provider != domain.ProviderMusicBrainz {
		t.Errorf("source provider: got %v, want %v", r.Sources[0].Provider, domain.ProviderMusicBrainz)
	}
	if r.Sources[0].ExternalID != "abc-123-def" {
		t.Errorf("source externalID: got %q, want %q", r.Sources[0].ExternalID, "abc-123-def")
	}
	if r.Sources[0].URL != "https://musicbrainz.org/recording/abc-123-def" {
		t.Errorf("source URL: got %q, want musicbrainz recording URL", r.Sources[0].URL)
	}
	if r.RecordingMBID != "abc-123-def" {
		t.Errorf("MBID: got %q, want %q", r.RecordingMBID, "abc-123-def")
	}
	if r.ISRC != "GBAYE9700011" {
		t.Errorf("ISRC: got %q, want %q", r.ISRC, "GBAYE9700011")
	}
}

func TestMusicBrainzAdapter_SearchStructured_Artists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ws/2/artist") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"artists": [{
				"id": "a74b1b7f-71a5-4011-9441-d0b5e4122711",
				"name": "Radiohead",
				"type": "Group",
				"area": {"name": "Oxford"},
				"tags": [
					{"name": "alternative rock", "count": 15},
					{"name": "electronic", "count": 8}
				]
			}]
		}`))
	}))
	defer server.Close()

	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	results, err := adapter.SearchStructured(context.Background(), "radiohead", "", map[domain.ResultKind]bool{
		domain.ResultKindArtist: true,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindArtist {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindArtist)
	}
	if r.Title != "Radiohead" {
		t.Errorf("title: got %q, want %q", r.Title, "Radiohead")
	}
	if r.Sources[0].Provider != domain.ProviderMusicBrainz {
		t.Errorf("source provider: got %v, want %v", r.Sources[0].Provider, domain.ProviderMusicBrainz)
	}
	if r.Sources[0].URL != "https://musicbrainz.org/artist/a74b1b7f-71a5-4011-9441-d0b5e4122711" {
		t.Errorf("source URL: got %q, want musicbrainz artist URL", r.Sources[0].URL)
	}
	if r.ArtistMBID != "a74b1b7f-71a5-4011-9441-d0b5e4122711" {
		t.Errorf("MBID: got %q, want the artist MBID", r.ArtistMBID)
	}
	if r.Extras["artist_type"] != "Group" {
		t.Errorf("extras.artist_type: got %v, want %q", r.Extras["artist_type"], "Group")
	}
	if r.Extras["area"] != "Oxford" {
		t.Errorf("extras.area: got %v, want %q", r.Extras["area"], "Oxford")
	}
	if r.Extras["mb_tags"] != "alternative rock, electronic" {
		t.Errorf("extras.mb_tags: got %v, want %q", r.Extras["mb_tags"], "alternative rock, electronic")
	}
}

func TestMusicBrainzAdapter_SearchStructured_ReleaseGroups(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ws/2/release-group") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"release-groups": [{
				"id": "rg-001-abc",
				"title": "OK Computer",
				"artist-credit": [{"name": "Radiohead"}]
			}]
		}`))
	}))
	defer server.Close()

	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	results, err := adapter.SearchStructured(context.Background(), "ok computer", "", map[domain.ResultKind]bool{
		domain.ResultKindAlbum: true,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Kind != domain.ResultKindAlbum {
		t.Errorf("kind: got %v, want %v", r.Kind, domain.ResultKindAlbum)
	}
	if r.Title != "OK Computer" {
		t.Errorf("title: got %q, want %q", r.Title, "OK Computer")
	}
	if r.Subtitle != "Radiohead" {
		t.Errorf("subtitle: got %q, want %q", r.Subtitle, "Radiohead")
	}
	if r.Sources[0].URL != "https://musicbrainz.org/release-group/rg-001-abc" {
		t.Errorf("source URL: got %q, want musicbrainz release-group URL", r.Sources[0].URL)
	}
	if r.ReleaseGroupMBID != "rg-001-abc" {
		t.Errorf("MBID: got %q, want %q", r.ReleaseGroupMBID, "rg-001-abc")
	}
}

func TestMusicBrainzAdapter_SearchStructured_RecordingsRequestISRCs(t *testing.T) {
	var receivedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/ws/2/recording") {
			w.Write([]byte(`{"recordings": []}`))
		} else if strings.HasPrefix(r.URL.Path, "/ws/2/artist") {
			w.Write([]byte(`{"artists": []}`))
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")

	t.Run("recording search includes inc=isrcs", func(t *testing.T) {
		receivedQuery = ""
		adapter.SearchStructured(context.Background(), "test", "", map[domain.ResultKind]bool{
			domain.ResultKindTrack: true,
		})
		if !strings.Contains(receivedQuery, "inc=isrcs") {
			t.Errorf("recording search must include inc=isrcs, got query: %s", receivedQuery)
		}
	})

	t.Run("artist search omits inc=isrcs", func(t *testing.T) {
		receivedQuery = ""
		adapter.SearchStructured(context.Background(), "test", "", map[domain.ResultKind]bool{
			domain.ResultKindArtist: true,
		})
		if strings.Contains(receivedQuery, "inc=isrcs") {
			t.Errorf("artist search must not include inc=isrcs, got query: %s", receivedQuery)
		}
	})
}

func TestMusicBrainzAdapter_ReleaseGroupTitles_paginates(t *testing.T) {
	var offsets []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ws/2/release-group") {
			http.NotFound(w, r)
			return
		}
		offsets = append(offsets, r.URL.Query().Get("offset"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("offset") == "0" {
			w.Write([]byte(`{
				"release-group-count": 3,
				"release-groups": [
					` + mbReleaseGroupJSON("rg-1", "First", "Che", "mbid-che") + `,
					` + mbReleaseGroupJSON("rg-2", "Second", "Che", "mbid-che") + `
				]
			}`))
			return
		}
		w.Write([]byte(`{
			"release-group-count": 3,
			"release-groups": [` + mbReleaseGroupJSON("rg-3", "Third", "Che", "mbid-che") + `]
		}`))
	}))
	defer server.Close()

	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	titles, err := adapter.ReleaseGroupTitles(context.Background(), "mbid-che")
	if err != nil {
		t.Fatalf("ReleaseGroupTitles: %v", err)
	}
	if len(offsets) != 2 || offsets[0] != "0" || offsets[1] != "100" {
		t.Errorf("offset params: got %v, want [0 100]", offsets)
	}
	if want := []string{"First", "Second", "Third"}; !slices.Equal(titles, want) {
		t.Errorf("titles = %v, want %v (pages appended in request order)", titles, want)
	}
}

func recordingArrivals(t *testing.T) (*httptest.Server, *[]time.Time) {
	t.Helper()
	var mu sync.Mutex
	var arrivals []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		arrivals = append(arrivals, time.Now())
		mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	return server, &arrivals
}

func lookupRecording(a *musicbrainz.MusicBrainzAdapter) func(context.Context) error {
	return func(ctx context.Context) error {
		_, _, err := a.RecordingByMBID(ctx, "rec-1")
		return err
	}
}

func TestMusicBrainzAdapter_rateLimit_cancelledCallersStillReserveOneSecondSlots(t *testing.T) {
	server, arrivals := recordingArrivals(t)
	lookup := lookupRecording(musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "test"))
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	for range 2 {
		if err := lookup(cancelled); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled lookup err = %v, want context.Canceled", err)
		}
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("cancelled-ctx callers blocked %v, want prompt return", elapsed)
	}
	if err := lookup(context.Background()); err != nil {
		t.Fatalf("live lookup: %v", err)
	}
	if len(*arrivals) != 1 {
		t.Fatalf("server saw %d requests, want only the live one", len(*arrivals))
	}
	if got := (*arrivals)[0].Sub(start); got < 1900*time.Millisecond || got > 3*time.Second {
		t.Errorf("live request reached MusicBrainz after %v, want ~2s (the two cancelled callers held the slots 1s apart)", got)
	}
}

func TestMusicBrainzAdapter_rateLimit_ctxCancelAbortsWait(t *testing.T) {
	server, _ := recordingArrivals(t)
	lookup := lookupRecording(musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "test"))
	if err := lookup(context.Background()); err != nil {
		t.Fatalf("first lookup: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_ = lookup(ctx)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("rateLimit blocked %v after ctx cancel, want prompt return", elapsed)
	}
}

func structuredQuerySent(t *testing.T, artist, track string, kind domain.ResultKind) (path, query string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	if _, err := adapter.SearchStructured(context.Background(), artist, track, map[domain.ResultKind]bool{kind: true}); err != nil {
		t.Fatalf("SearchStructured: %v", err)
	}
	return path, query
}

func TestMusicBrainzAdapter_SearchStructured_queryPerKind(t *testing.T) {
	tests := []struct {
		name, path, want string
		kind             domain.ResultKind
	}{
		{"track", "/ws/2/recording/", `artist:"Queen" AND recording:"Bohemian Rhapsody"`, domain.ResultKindTrack},
		{"album", "/ws/2/release-group/", `artist:"Queen" AND release:"Bohemian Rhapsody"`, domain.ResultKindAlbum},
		{"artist ignores track", "/ws/2/artist/", "Queen", domain.ResultKindArtist},
		{"unknown falls back to concatenation", "/ws/2/recording/", "Queen Bohemian Rhapsody", domain.ResultKindUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, query := structuredQuerySent(t, "Queen", "Bohemian Rhapsody", tt.kind)
			if path != tt.path || query != tt.want {
				t.Errorf("request = %s query %q, want %s query %q", path, query, tt.path, tt.want)
			}
		})
	}
}

func TestMusicBrainzAdapter_SearchStructured_failedKindSkipped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/ws/2/recording"):
			_, _ = w.Write([]byte(`{"recordings": [{"id": "rec-1", "title": "Söz 🎵 東京", "artist-credit": [{"name": "Queen"}]}]}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	results, err := adapter.SearchStructured(context.Background(), "Queen", "Söz", map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindArtist: true,
	})
	if err != nil {
		t.Fatalf("SearchStructured must not fail when one kind fails: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected the surviving kind's 1 result, got %d", len(results))
	}
	if results[0].Title != "Söz 🎵 東京" {
		t.Errorf("title = %q, want the unicode title preserved", results[0].Title)
	}
}

func TestMusicBrainzAdapter_ResolveArtistIdentity(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"artists": [
			{"id": "mbid-other", "name": "Someone Else"},
			{"id": "mbid-che-1", "name": "Che", "disambiguation": "US rapper", "type": "Person",
			 "area": {"name": "Atlanta"}, "life-span": {"begin": "2004-05-01"}},
			{"id": "mbid-che-2", "name": "Che", "disambiguation": "UK band"}
		]}`))
	}))
	defer server.Close()

	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	id, err := adapter.ResolveArtistIdentity(context.Background(), "Che")
	if err != nil {
		t.Fatalf("ResolveArtistIdentity: %v", err)
	}
	if id == nil {
		t.Fatal("expected an identity for an exact name match")
	}
	if id.MBID != "mbid-che-1" {
		t.Errorf("MBID = %q, want the FIRST exact name match", id.MBID)
	}
	if id.Disambiguation != "US rapper" || id.Area != "Atlanta" || id.ArtistType != "Person" {
		t.Errorf("identity = %+v, want disambiguation/area/type mapped", id)
	}
	if id.BirthYear != 2004 {
		t.Errorf("BirthYear = %d, want 2004 (parsed from life-span.begin)", id.BirthYear)
	}

	before := requests
	if _, err := adapter.ResolveArtistIdentity(context.Background(), "Che"); err != nil {
		t.Fatalf("memoized ResolveArtistIdentity: %v", err)
	}
	if requests != before {
		t.Errorf("requests = %d, want %d (identity memo must absorb the repeat)", requests, before)
	}
}

func TestMusicBrainzAdapter_ResolveArtistIdentity_noExactMatchIsNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"artists": [{"id": "mbid-x", "name": "Radiohead Tribute Band"}]}`))
	}))
	defer server.Close()

	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	id, err := adapter.ResolveArtistIdentity(context.Background(), "Radiohead")
	if !errors.Is(err, ports.ErrArtistIdentityNotFound) {
		t.Fatalf("ResolveArtistIdentity error = %v, want ErrArtistIdentityNotFound when no result matches the name exactly", err)
	}
	if id != nil {
		t.Errorf("identity = %+v, want nil", id)
	}
}

func TestMusicBrainzAdapter_ResolveArtistIdentity_birthYearFromLifeSpanBegin(t *testing.T) {
	tests := []struct {
		begin string
		want  int
	}{
		{"1969-10-02", 1969},
		{"2004", 2004},
		{"", 0},
		{"196", 0},
		{"19x9-01", 0},
		{"/969", 0},
		{":969", 0},
	}
	for _, tt := range tests {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"artists": [{"id": "mbid-che", "name": "Che", "life-span": {"begin": "` + tt.begin + `"}}]}`))
		}))
		adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
		id, err := adapter.ResolveArtistIdentity(context.Background(), "Che")
		server.Close()
		if err != nil || id == nil {
			t.Fatalf("ResolveArtistIdentity(begin %q) = %v, %v; want an identity", tt.begin, id, err)
		}
		if id.BirthYear != tt.want {
			t.Errorf("BirthYear for begin %q = %d, want %d", tt.begin, id.BirthYear, tt.want)
		}
	}
}

func TestMusicBrainzAdapter_ListArtistDiscography(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/ws/2/artist"):
			_, _ = w.Write([]byte(`{"artists": [{"id": "mbid-rh", "name": "Radiohead"}]}`))
		case strings.HasPrefix(r.URL.Path, "/ws/2/release-group"):
			_, _ = w.Write([]byte(`{
				"release-group-count": 1,
				"release-groups": [{
					"id": "rg-okc", "title": "OK Computer", "primary-type": "Album",
					"first-release-date": "1997-05-21",
					"artist-credit": [{"name": "Radiohead"}]
				}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	results, err := adapter.ListArtistDiscography(context.Background(), "Radiohead")
	if err != nil {
		t.Fatalf("ListArtistDiscography: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 album, got %d", len(results))
	}
	r := results[0]
	if r.Kind != domain.ResultKindAlbum || r.Title != "OK Computer" {
		t.Errorf("result = %+v, want the mapped release-group", r)
	}
	if r.ReleaseDate != "1997-05-21" {
		t.Errorf("ReleaseDate = %q, want first-release-date carried", r.ReleaseDate)
	}
	if r.RecordType != "album" {
		t.Errorf("record_type = %v, want %q (lower-cased primary-type)", r.RecordType, "album")
	}
}

func TestMusicBrainzAdapter_ListArtistDiscography_unknownArtistIsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"artists": []}`))
	}))
	defer server.Close()

	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	results, err := adapter.ListArtistDiscography(context.Background(), "Nobody")
	if err != nil {
		t.Fatalf("ListArtistDiscography: %v", err)
	}
	if results != nil {
		t.Errorf("results = %+v, want nil for an unknown artist (clean miss, not error)", results)
	}
}

func TestMusicBrainzAdapter_ReleaseGroupTitles_emptyMBIDNoRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("no HTTP request expected for an empty mbid")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	titles, err := adapter.ReleaseGroupTitles(context.Background(), "")
	if err != nil || titles != nil {
		t.Errorf("ReleaseGroupTitles(\"\") = (%v, %v), want (nil, nil)", titles, err)
	}
}

func TestMusicBrainzAdapter_ListArtistDiscography_laterPageErrorKeepsEarlierPagesAndIsNotMemoized(t *testing.T) {
	var firstPages atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/ws/2/artist"):
			_, _ = w.Write([]byte(`{"artists": [{"id": "mbid-1", "name": "Che"}]}`))
		case strings.HasPrefix(r.URL.Path, "/ws/2/release-group") && r.URL.Query().Get("offset") == "0":
			firstPages.Add(1)
			_, _ = w.Write([]byte(`{
				"release-groups": [{"id": "rg-1", "title": "One", "primary-type": "Album"}],
				"release-group-count": 150
			}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	for call := 1; call <= 2; call++ {
		results, err := adapter.ListArtistDiscography(context.Background(), "Che")
		var partial *domain.PartialResultError
		if !errors.As(err, &partial) || partial.Page != 1 {
			t.Fatalf("call %d: expected a PartialResultError for page 1 alongside the earlier pages, got: %v", call, err)
		}
		if len(results) != 1 || results[0].Title != "One" {
			t.Fatalf("call %d: results = %+v, want the 1 page-1 release-group kept", call, results)
		}
	}
	if got := firstPages.Load(); got != 2 {
		t.Errorf("first page fetched %d times, want 2 (a truncated discography must not be memoized and reused for hours)", got)
	}
}

func TestMusicBrainzAdapter_ReleaseGroupTitles_singleflightCollapsesConcurrentFetches(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ws/2/release-group") {
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		_, _ = w.Write([]byte(`{
			"release-groups": [{"id": "rg-1", "title": "One", "primary-type": "Album"}],
			"release-group-count": 1
		}`))
	}))
	defer server.Close()

	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			titles, err := adapter.ReleaseGroupTitles(context.Background(), "mbid-1")
			if err != nil || len(titles) != 1 {
				t.Errorf("ReleaseGroupTitles = (%v, %v), want 1 title", titles, err)
			}
		}()
	}
	wg.Wait()
	if got := requests.Load(); got != 1 {
		t.Errorf("underlying MB requests = %d, want 1 (concurrent detail-opens must collapse)", got)
	}
}

func TestMusicBrainzAdapter_SearchStructured_escapesEmbeddedQuotes(t *testing.T) {
	_, got := structuredQuerySent(t, `The "Best" Band`, `Hello`, domain.ResultKindTrack)
	want := `artist:"The \"Best\" Band" AND recording:"Hello"`
	if got != want {
		t.Errorf("query = %q, want %q (embedded quotes backslash-escaped)", got, want)
	}
}

func albumSearchResult(t *testing.T, releaseGroup string) domain.SearchResult {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"release-groups": [` + releaseGroup + `]}`))
	}))
	defer server.Close()
	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	results, err := adapter.SearchStructured(context.Background(), "Che", "Some EP", providertest.AlbumKinds())
	if err != nil || len(results) != 1 {
		t.Fatalf("SearchStructured = %d results, %v; want 1 album", len(results), err)
	}
	return results[0]
}

func TestMusicBrainzAdapter_SearchStructured_albumCarriesFirstReleaseDate(t *testing.T) {
	r := albumSearchResult(t, `{"id": "rg-1", "title": "Some EP", "primary-type": "EP",
		"first-release-date": "2019-05-01", "artist-credit": [{"name": "Che"}]}`)
	if r.ReleaseDate != "2019-05-01" {
		t.Errorf("ReleaseDate = %q, want 2019-05-01 (MB first-release-date must map through)", r.ReleaseDate)
	}
	if r.Subtitle != "Che" {
		t.Errorf("Subtitle = %q, want Che", r.Subtitle)
	}
	if r.ReleaseGroupMBID != "rg-1" {
		t.Errorf("MBID = %q, want rg-1", r.ReleaseGroupMBID)
	}
	if r.RecordType != "ep" {
		t.Errorf("record_type = %v, want ep (lowercased primary-type, so EPs leave the Albums row)", r.RecordType)
	}
}

func TestMusicBrainzAdapter_SearchStructured_albumKeepsAPartialDate(t *testing.T) {
	r := albumSearchResult(t, `{"id": "rg-2", "title": "Early", "first-release-date": "2005"}`)
	if r.ReleaseDate != "2005" {
		t.Errorf("ReleaseDate = %q, want 2005", r.ReleaseDate)
	}
}

func TestMusicBrainzAdapter_SearchStructured_escapesLuceneMetacharacters(t *testing.T) {
	var rawQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"artists": []}`))
	}))
	defer server.Close()

	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	_, err := adapter.SearchStructured(context.Background(), "*NSYNC", "", map[domain.ResultKind]bool{
		domain.ResultKindArtist: true,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	q, err := url.QueryUnescape(strings.Split(rawQuery, "&")[0])
	if err != nil {
		t.Fatalf("QueryUnescape: %v", err)
	}
	q = strings.TrimPrefix(q, "query=")
	if q != `\*NSYNC` {
		t.Errorf("query sent to MusicBrainz = %q, want the leading * backslash-escaped so it is not Lucene wildcard syntax", q)
	}
}

func TestMusicBrainzAdapter_ResolveArtistIdentity_escapesLuceneMetacharacters(t *testing.T) {
	var rawQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"artists": []}`))
	}))
	defer server.Close()

	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	if _, err := adapter.ResolveArtistIdentity(context.Background(), "Sunn O)))"); !errors.Is(err, ports.ErrArtistIdentityNotFound) {
		t.Fatalf("ResolveArtistIdentity: %v", err)
	}

	q, err := url.QueryUnescape(strings.Split(rawQuery, "&")[0])
	if err != nil {
		t.Fatalf("QueryUnescape: %v", err)
	}
	q = strings.TrimPrefix(q, "query=")
	if q != `Sunn O\)\)\)` {
		t.Errorf("query sent to MusicBrainz = %q, want the unbalanced parens backslash-escaped", q)
	}
}

func resolveMBIDQuery(t *testing.T, kind domain.ResultKind, title, body string) string {
	t.Helper()
	var rawQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	if _, err := adapter.ResolveMBID(context.Background(), kind, title, ""); err != nil {
		t.Fatalf("ResolveMBID: %v", err)
	}
	q, err := url.QueryUnescape(strings.Split(rawQuery, "&")[0])
	if err != nil {
		t.Fatalf("QueryUnescape: %v", err)
	}
	return strings.TrimPrefix(q, "query=")
}

func TestMusicBrainzAdapter_ResolveMBID_trackEscapesLuceneMetacharacters(t *testing.T) {
	if q := resolveMBIDQuery(t, domain.ResultKindTrack, `Track\`, `{"recordings": []}`); q != `Track\\` {
		t.Errorf("query sent to MusicBrainz = %q, want the trailing backslash escaped so the phrase terminates", q)
	}
}

func TestMusicBrainzAdapter_ResolveMBID_albumEscapesLuceneMetacharacters(t *testing.T) {
	if q := resolveMBIDQuery(t, domain.ResultKindAlbum, "genre:rock", `{"release-groups": []}`); q != `genre\:rock` {
		t.Errorf("query sent to MusicBrainz = %q, want the field-colon backslash-escaped", q)
	}
}

func TestMusicBrainzAdapter_SearchStructured_escapesArtistOnlyAndDefaultBranches(t *testing.T) {
	if _, got := structuredQuerySent(t, "*NSYNC", "Bye Bye Bye", domain.ResultKindArtist); got != `\*NSYNC` {
		t.Errorf("artist branch = %q, want %q", got, `\*NSYNC`)
	}
	if _, got := structuredQuerySent(t, "Sunn O)))", "Bye Bye Bye", domain.ResultKindUnknown); got != `Sunn O\)\)\) Bye Bye Bye` {
		t.Errorf("default branch = %q, want %q", got, `Sunn O\)\)\) Bye Bye Bye`)
	}
}

func TestMusicBrainzAdapter_SearchStructured_escapesTrackAndAlbumBranches(t *testing.T) {
	if _, got := structuredQuerySent(t, "*NSYNC", `Bye\`, domain.ResultKindTrack); got != `artist:"\*NSYNC" AND recording:"Bye\\"` {
		t.Errorf("track branch = %q, want leading * and trailing backslash backslash-escaped", got)
	}
	if _, got := structuredQuerySent(t, "Sunn O)))", "Bye Bye Bye", domain.ResultKindAlbum); got != `artist:"Sunn O\)\)\)" AND release:"Bye Bye Bye"` {
		t.Errorf("album branch = %q, want unbalanced parens backslash-escaped", got)
	}
}

func TestMusicBrainzAdapter_rateLimit_expiredOnArrivalIsNotQueueTimeout(t *testing.T) {
	server, arrivals := recordingArrivals(t)
	lookup := lookupRecording(musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "test"))
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	err := lookup(ctx)

	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) {
		t.Fatalf("want plain DeadlineExceeded, got %v", err)
	}
	if len(*arrivals) != 0 {
		t.Errorf("server saw %d requests, want none for an expired caller", len(*arrivals))
	}
}

func TestProviderLimiterBurstShedsBeyondQueueDepth(t *testing.T) {
	const callers = 20
	burstServer, _ := recordingArrivals(t)
	cases := []struct {
		name  string
		wait  func(context.Context) error
		burst int
	}{
		{"musicbrainz", lookupRecording(musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(burstServer.URL), "ua")), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := providertest.FireBurst(t, tc.wait, callers, 300*time.Millisecond)

			admitted, shed := 0, 0
			for _, r := range got {
				switch {
				case r.Err == nil:
					admitted++
				case errors.Is(r.Err, ports.ErrProviderRateLimitQueueTimeout):
					shed++
				default:
					t.Errorf("caller got %v, want nil or a queue-timeout shed", r.Err)
				}
			}
			if admitted != tc.burst {
				t.Errorf("admitted at once = %d, want the burst of %d", admitted, tc.burst)
			}
			if want := callers - tc.burst - providerrequests.ProviderQueueDepth; shed != want {
				t.Errorf("shed within 300ms = %d, want %d (callers %d - burst %d - queue depth %d)",
					shed, want, callers, tc.burst, providerrequests.ProviderQueueDepth)
			}
		})
	}
}

func trackSearchResults(t *testing.T, recordings string) []domain.SearchResult {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"recordings": [` + recordings + `]}`))
	}))
	defer server.Close()
	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	results, err := adapter.SearchStructured(context.Background(), "Rush", "Tom Sawyer", providertest.TrackKinds())
	if err != nil {
		t.Fatalf("SearchStructured: %v", err)
	}
	return results
}

func TestMusicBrainzAdapter_SearchStructured_aLiveRecordingDoesNotPassAsTheStudioCut(t *testing.T) {
	results := trackSearchResults(t, `
		{"id": "rec-1", "title": "Tom Sawyer", "disambiguation": "live, 2015‐06‐17 & 19: Air Canada Centre, Toronto, ON, Canada", "length": 300000, "isrcs": ["CAA311502074"]},
		{"id": "rec-2", "title": "Tom Sawyer", "length": 276000}`)
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if got := results[0].Title; got != "Tom Sawyer (live, 2015‐06‐17 & 19: Air Canada Centre, Toronto, ON, Canada)" {
		t.Errorf("Title = %q, want the disambiguation shown", got)
	}
	if got := results[1].Title; got != "Tom Sawyer" {
		t.Errorf("Title = %q, want the plain title", got)
	}
}
