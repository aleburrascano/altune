package musicbrainz

import (
	"altune/go-api/internal/discovery/adapters/providers/providerhttp"
	"altune/go-api/internal/discovery/adapters/providers/providertest"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service/providercall"
	"altune/go-api/internal/discovery/service/search"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestMusicBrainzAdapter_Search_Recordings(t *testing.T) {
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

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	results, err := adapter.Search(context.Background(), "paranoid android", map[domain.ResultKind]bool{
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

func TestMusicBrainzAdapter_Search_Artists(t *testing.T) {
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

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	results, err := adapter.Search(context.Background(), "radiohead", map[domain.ResultKind]bool{
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

func TestMusicBrainzAdapter_Search_ReleaseGroups(t *testing.T) {
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

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	results, err := adapter.Search(context.Background(), "ok computer", map[domain.ResultKind]bool{
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

func TestMusicBrainzAdapter_Search_RecordingsRequestISRCs(t *testing.T) {
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

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")

	t.Run("recording search includes inc=isrcs", func(t *testing.T) {
		receivedQuery = ""
		adapter.Search(context.Background(), "test", map[domain.ResultKind]bool{
			domain.ResultKindTrack: true,
		})
		if !strings.Contains(receivedQuery, "inc=isrcs") {
			t.Errorf("recording search must include inc=isrcs, got query: %s", receivedQuery)
		}
	})

	t.Run("artist search omits inc=isrcs", func(t *testing.T) {
		receivedQuery = ""
		adapter.Search(context.Background(), "test", map[domain.ResultKind]bool{
			domain.ResultKindArtist: true,
		})
		if strings.Contains(receivedQuery, "inc=isrcs") {
			t.Errorf("artist search must not include inc=isrcs, got query: %s", receivedQuery)
		}
	})
}

func TestMusicBrainzAdapter_Search_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
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

func TestMusicBrainzAdapter_fetchReleaseGroups_paginates(t *testing.T) {
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

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	rgs, err := adapter.fetchReleaseGroups(context.Background(), "mbid-che")
	if err != nil {
		t.Fatalf("fetchReleaseGroups: %v", err)
	}
	if len(offsets) != 2 || offsets[0] != "0" || offsets[1] != "100" {
		t.Errorf("offset params: got %v, want [0 100]", offsets)
	}
	if len(rgs) != 3 {
		t.Fatalf("expected 3 release-groups across pages, got %d", len(rgs))
	}
	for i, want := range []string{"First", "Second", "Third"} {
		if rgs[i].Title != want {
			t.Errorf("rg[%d]: got %q, want %q (pages appended in request order)", i, rgs[i].Title, want)
		}
	}
}

func TestMusicBrainzAdapter_rateLimit_reservesDistinctSlots(t *testing.T) {
	a := NewMusicBrainzAdapter(http.DefaultClient, "test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_ = a.limiter.Wait(ctx)
	_ = a.limiter.Wait(ctx)
	_ = a.limiter.Wait(ctx)

	a.limiter.Mu.Lock()
	last := a.limiter.LastReq
	a.limiter.Mu.Unlock()

	if got := last.Sub(start); got < 1900*time.Millisecond || got > 3*time.Second {
		t.Errorf("lastReq advanced by %v, want ~2s (three callers spaced 1s apart)", got)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("cancelled-ctx callers blocked %v, want prompt return", elapsed)
	}
}

func TestMusicBrainzAdapter_rateLimit_ctxCancelAbortsWait(t *testing.T) {
	a := NewMusicBrainzAdapter(http.DefaultClient, "test")
	a.limiter.Mu.Lock()
	a.limiter.LastReq = time.Now()
	a.limiter.Mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_ = a.limiter.Wait(ctx)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("rateLimit blocked %v after ctx cancel, want prompt return", elapsed)
	}
}

func TestMBStructuredQuery_perKind(t *testing.T) {
	tests := []struct {
		name string
		kind domain.ResultKind
		want string
	}{
		{"track", domain.ResultKindTrack, `artist:"Queen" AND recording:"Bohemian Rhapsody"`},
		{"album", domain.ResultKindAlbum, `artist:"Queen" AND release:"Bohemian Rhapsody"`},
		{"artist ignores track", domain.ResultKindArtist, "Queen"},
		{"unknown falls back to concatenation", domain.ResultKindUnknown, "Queen Bohemian Rhapsody"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mbStructuredQuery("Queen", "Bohemian Rhapsody", tt.kind); got != tt.want {
				t.Errorf("query = %q, want %q", got, tt.want)
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

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
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

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
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

func TestMusicBrainzAdapter_ResolveArtistIdentity_noExactMatchIsNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"artists": [{"id": "mbid-x", "name": "Radiohead Tribute Band"}]}`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	id, err := adapter.ResolveArtistIdentity(context.Background(), "Radiohead")
	if err != nil {
		t.Fatalf("ResolveArtistIdentity: %v", err)
	}
	if id != nil {
		t.Errorf("identity = %+v, want nil when no result matches the name exactly", id)
	}
}

func TestParseBirthYear(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"1969-10-02", 1969},
		{"2004", 2004},
		{"", 0},
		{"196", 0},
		{"19x9-01", 0},
	}
	for _, tt := range tests {
		if got := parseBirthYear(tt.in); got != tt.want {
			t.Errorf("parseBirthYear(%q) = %d, want %d", tt.in, got, tt.want)
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

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
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

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
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

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	titles, err := adapter.ReleaseGroupTitles(context.Background(), "")
	if err != nil || titles != nil {
		t.Errorf("ReleaseGroupTitles(\"\") = (%v, %v), want (nil, nil)", titles, err)
	}
}

func TestMusicBrainzAdapter_Search_malformedJSONIsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`<html>rate limited</html>`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	_, err := adapter.Search(context.Background(), "anything", map[domain.ResultKind]bool{
		domain.ResultKindTrack: true,
	})
	if err == nil {
		t.Fatal("expected an error on an HTML-instead-of-JSON body (MB 503 page)")
	}
}

func TestMusicBrainz_FetchReleaseGroups_laterPageErrorKeepsEarlierPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/ws/2/release-group") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("offset") != "0" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{
			"release-groups": [{"id": "rg-1", "title": "One", "primary-type": "Album"}],
			"release-group-count": 150
		}`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	rgs, err := adapter.fetchReleaseGroups(context.Background(), "mbid-1")
	var partial *domain.PartialResultError
	if !errors.As(err, &partial) || partial.Page != 1 {
		t.Fatalf("expected a PartialResultError for page 1 alongside the earlier pages, got: %v", err)
	}
	if len(rgs) != 1 || rgs[0].Title != "One" {
		t.Fatalf("rgs = %+v, want the 1 page-1 release-group kept", rgs)
	}
	if _, ok := adapter.releaseMemo.get("mbid-1"); ok {
		t.Error("partial release-group set was memoized; a truncated discography would be reused for hours")
	}
}

func TestMusicBrainz_FetchReleaseGroups_singleflightCollapsesConcurrentFetches(t *testing.T) {
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

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rgs, err := adapter.fetchReleaseGroups(context.Background(), "mbid-1")
			if err != nil || len(rgs) != 1 {
				t.Errorf("fetchReleaseGroups = (%v, %v), want 1 release-group", rgs, err)
			}
		}()
	}
	wg.Wait()
	if got := requests.Load(); got != 1 {
		t.Errorf("underlying MB requests = %d, want 1 (concurrent detail-opens must collapse)", got)
	}
}

func TestMBStructuredQuery_escapesEmbeddedQuotes(t *testing.T) {
	got := mbStructuredQuery(`The "Best" Band`, `Hello`, domain.ResultKindTrack)
	want := `artist:"The \"Best\" Band" AND recording:"Hello"`
	if got != want {
		t.Errorf("query = %q, want %q (embedded quotes backslash-escaped)", got, want)
	}
}

func TestMapMBReleaseGroup_carriesFirstReleaseDate(t *testing.T) {
	r := mapMBReleaseGroup(mbReleaseGroup{
		ID:               "rg-1",
		Title:            "Some EP",
		PrimaryType:      "EP",
		FirstReleaseDate: "2019-05-01",
		ArtistCredit:     []mbArtistRef{{Name: "Che"}},
	})
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

func TestMapMBReleaseGroup_partialDate(t *testing.T) {
	r := mapMBReleaseGroup(mbReleaseGroup{ID: "rg-2", Title: "Early", FirstReleaseDate: "2005"})
	if r.ReleaseDate != "2005" {
		t.Errorf("ReleaseDate = %q, want 2005", r.ReleaseDate)
	}
}

func TestMusicBrainzAdapter_Search_escapesLuceneMetacharacters(t *testing.T) {
	var rawQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"artists": []}`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	_, err := adapter.Search(context.Background(), "*NSYNC", map[domain.ResultKind]bool{
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

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	if _, err := adapter.ResolveArtistIdentity(context.Background(), "Sunn O)))"); err != nil {
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

func TestMusicBrainzAdapter_fetchRecordingMatches_escapesLuceneMetacharacters(t *testing.T) {
	var rawQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"recordings": []}`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	if _, err := adapter.fetchRecordingMatches(context.Background(), `Track\`); err != nil {
		t.Fatalf("fetchRecordingMatches: %v", err)
	}

	q, err := url.QueryUnescape(strings.Split(rawQuery, "&")[0])
	if err != nil {
		t.Fatalf("QueryUnescape: %v", err)
	}
	q = strings.TrimPrefix(q, "query=")
	if q != `Track\\` {
		t.Errorf("query sent to MusicBrainz = %q, want the trailing backslash escaped so the phrase terminates", q)
	}
}

func TestMusicBrainzAdapter_fetchReleaseGroupMatches_escapesLuceneMetacharacters(t *testing.T) {
	var rawQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"release-groups": []}`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0")
	if _, err := adapter.fetchReleaseGroupMatches(context.Background(), "genre:rock"); err != nil {
		t.Fatalf("fetchReleaseGroupMatches: %v", err)
	}

	q, err := url.QueryUnescape(strings.Split(rawQuery, "&")[0])
	if err != nil {
		t.Fatalf("QueryUnescape: %v", err)
	}
	q = strings.TrimPrefix(q, "query=")
	if q != `genre\:rock` {
		t.Errorf("query sent to MusicBrainz = %q, want the field-colon backslash-escaped", q)
	}
}

func TestMBStructuredQuery_escapesArtistOnlyAndDefaultBranches(t *testing.T) {
	if got, want := mbStructuredQuery("*NSYNC", "Bye Bye Bye", domain.ResultKindArtist), `\*NSYNC`; got != want {
		t.Errorf("artist branch = %q, want %q", got, want)
	}
	if got, want := mbStructuredQuery("Sunn O)))", "Bye Bye Bye", domain.ResultKindUnknown), `Sunn O\)\)\) Bye Bye Bye`; got != want {
		t.Errorf("default branch = %q, want %q", got, want)
	}
}

func TestMBStructuredQuery_escapesTrackAndAlbumBranches(t *testing.T) {
	if got, want := mbStructuredQuery("*NSYNC", `Bye\`, domain.ResultKindTrack), `artist:"\*NSYNC" AND recording:"Bye\\"`; got != want {
		t.Errorf("track branch = %q, want %q (leading * and trailing backslash backslash-escaped)", got, want)
	}
	if got, want := mbStructuredQuery("Sunn O)))", "Bye Bye Bye", domain.ResultKindAlbum), `artist:"Sunn O\)\)\)" AND release:"Bye Bye Bye"`; got != want {
		t.Errorf("album branch = %q, want %q (unbalanced parens backslash-escaped)", got, want)
	}
}

func TestMinIntervalLimiterExpiredOnArrivalIsNotQueueTimeout(t *testing.T) {
	l := providerhttp.NewMinIntervalLimiter(time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	err := l.Wait(ctx)

	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) {
		t.Fatalf("want plain DeadlineExceeded, got %v", err)
	}
}

type shortBudgetMB struct {
	*MusicBrainzAdapter
	budget time.Duration
}

func (a shortBudgetMB) SearchTimeout() time.Duration { return a.budget }

func newShortBudgetMB(serverURL string, interval, budget time.Duration) shortBudgetMB {
	mb := NewMusicBrainzAdapter(providertest.NewClient(serverURL), "altune-test/1.0")
	mb.limiter = providerhttp.NewMinIntervalLimiter(interval)
	return shortBudgetMB{MusicBrainzAdapter: mb, budget: budget}
}

var trackOnly = map[domain.ResultKind]bool{domain.ResultKindTrack: true}

func healthyMBServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"recordings":[{"id":"rec-1","title":"Humble","artist-credit":[{"name":"Kendrick Lamar"}]}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func inspect(t *testing.T, svc *search.Service) {
	inspectKinds(t, svc, trackOnly)
}

func inspectKinds(t *testing.T, svc *search.Service, kinds map[domain.ResultKind]bool) {
	q, err := domain.NewSearchQuery("humble", kinds, 10)
	if err != nil {
		t.Errorf("query: %v", err)
		return
	}
	_, _ = svc.InspectSearchWithStatuses(context.Background(), q)
}

func searchConcurrently(t *testing.T, svc *search.Service, n int) {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			inspect(t, svc)
		}()
	}
	wg.Wait()
}

func TestRateLimitQueueTimeoutsDoNotOpenCircuit(t *testing.T) {
	srv := healthyMBServer(t)
	mb := newShortBudgetMB(srv.URL, 200*time.Millisecond, 50*time.Millisecond)
	cb := providercall.NewCircuitBreaker()
	svc := search.NewService([]ports.SearchProvider{mb}, cb)
	inspect(t, svc)

	searchConcurrently(t, svc, 10)

	if got := cb.GetStatus(domain.ProviderMusicBrainz); got != domain.ProviderStatusOK {
		t.Fatalf("circuit status = %v after a queue-only overload of a healthy provider, want ok", got)
	}
}

func TestRateLimitQueueTimeoutIsShedEarly(t *testing.T) {
	srv := healthyMBServer(t)
	mb := newShortBudgetMB(srv.URL, time.Hour, time.Second)
	if _, err := mb.Search(context.Background(), "humble", trackOnly); err != nil {
		t.Fatalf("first search: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	_, err := mb.Search(ctx, "humble", trackOnly)
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("queued search took %v, want it shed immediately", elapsed)
	}
	if !errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) {
		t.Errorf("err = %v, want ports.ErrProviderRateLimitQueueTimeout", err)
	}
}

func TestRealUpstreamTimeoutStillOpensCircuit(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	mb := newShortBudgetMB(srv.URL, time.Millisecond, 30*time.Millisecond)
	cb := providercall.NewCircuitBreaker()
	svc := search.NewService([]ports.SearchProvider{mb}, cb)

	allKinds := map[domain.ResultKind]bool{
		domain.ResultKindTrack: true, domain.ResultKindAlbum: true, domain.ResultKindArtist: true,
	}
	for i := 0; i < 5; i++ {
		inspectKinds(t, svc, allKinds)
	}

	if got := cb.GetStatus(domain.ProviderMusicBrainz); got != domain.ProviderStatusCircuitOpen {
		t.Fatalf("circuit status = %v after 5 upstream timeouts, want circuit_open", got)
	}
}

func TestUpstream5xxStillOpensCircuit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	mb := newShortBudgetMB(srv.URL, time.Millisecond, time.Second)
	cb := providercall.NewCircuitBreaker()
	svc := search.NewService([]ports.SearchProvider{mb}, cb)

	for i := 0; i < 5; i++ {
		inspect(t, svc)
	}

	if got := cb.GetStatus(domain.ProviderMusicBrainz); got != domain.ProviderStatusCircuitOpen {
		t.Fatalf("circuit status = %v after 5 upstream 503s, want circuit_open", got)
	}
}

func TestRateLimitQueueFullShedsDoNotOpenCircuit(t *testing.T) {
	srv := healthyMBServer(t)
	mb := newShortBudgetMB(srv.URL, 100*time.Millisecond, time.Minute)
	cb := providercall.NewCircuitBreaker()
	svc := search.NewService([]ports.SearchProvider{mb}, cb)
	q, err := domain.NewSearchQuery("humble", trackOnly, 10)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	const searches = 12
	statuses := make(chan domain.ProviderStatus, searches)
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < searches; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, st := svc.InspectSearchWithStatuses(context.Background(), q)
			for _, s := range st {
				statuses <- s.Status
			}
		}()
	}
	wg.Wait()
	close(statuses)

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("burst took %v, want excess searches shed rather than queued", elapsed)
	}
	counts := map[domain.ProviderStatus]int{}
	for s := range statuses {
		counts[s]++
	}
	if want := 1 + providerhttp.ProviderQueueDepth; counts[domain.ProviderStatusOK] != want {
		t.Errorf("ok searches = %d, want %d (1 immediate + queue depth); counts %v",
			counts[domain.ProviderStatusOK], want, counts)
	}
	if want := searches - 1 - providerhttp.ProviderQueueDepth; counts[domain.ProviderStatusTimeout] != want {
		t.Errorf("timeout searches = %d, want %d shed; counts %v",
			counts[domain.ProviderStatusTimeout], want, counts)
	}
	if got := cb.GetStatus(domain.ProviderMusicBrainz); got != domain.ProviderStatusOK {
		t.Fatalf("circuit status = %v after a queue-full overload of a healthy provider, want ok", got)
	}
}

func TestProviderLimiterBurstShedsBeyondQueueDepth(t *testing.T) {
	const callers = 20
	cases := []struct {
		name  string
		wait  func(context.Context) error
		burst int
	}{
		{"musicbrainz", NewMusicBrainzAdapter(http.DefaultClient, "ua").limiter.Wait, 1},
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
			if want := callers - tc.burst - providerhttp.ProviderQueueDepth; shed != want {
				t.Errorf("shed within 300ms = %d, want %d (callers %d - burst %d - queue depth %d)",
					shed, want, callers, tc.burst, providerhttp.ProviderQueueDepth)
			}
		})
	}
}

func TestMapMBRecording_ALiveRecordingDoesNotPassAsTheStudioCut(t *testing.T) {
	live := mbRecording{ID: "rec-1", Title: "Tom Sawyer", Disambiguation: "live, 2015‐06‐17 & 19: Air Canada Centre, Toronto, ON, Canada", LengthMs: 300000, ISRCs: []string{"CAA311502074"}}
	if got := mapMBRecording(live); got.Title != "Tom Sawyer (live, 2015‐06‐17 & 19: Air Canada Centre, Toronto, ON, Canada)" {
		t.Errorf("Title = %q, want the disambiguation shown", got.Title)
	}
	studio := mbRecording{ID: "rec-2", Title: "Tom Sawyer", LengthMs: 276000}
	if got := mapMBRecording(studio); got.Title != "Tom Sawyer" {
		t.Errorf("Title = %q, want the plain title", got.Title)
	}
}
