package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"net/http"
	"net/http/httptest"
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

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
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
	if r.MBID != "abc-123-def" {
		t.Errorf("MBID: got %q, want %q", r.MBID, "abc-123-def")
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

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
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
	if r.MBID != "a74b1b7f-71a5-4011-9441-d0b5e4122711" {
		t.Errorf("MBID: got %q, want the artist MBID", r.MBID)
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

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
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
	if r.MBID != "rg-001-abc" {
		t.Errorf("MBID: got %q, want %q", r.MBID, "rg-001-abc")
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

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")

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

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
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

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
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
	_ = a.limiter.wait(ctx)
	_ = a.limiter.wait(ctx)
	_ = a.limiter.wait(ctx)

	a.limiter.mu.Lock()
	last := a.limiter.lastReq
	a.limiter.mu.Unlock()

	if got := last.Sub(start); got < 1900*time.Millisecond || got > 3*time.Second {
		t.Errorf("lastReq advanced by %v, want ~2s (three callers spaced 1s apart)", got)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("cancelled-ctx callers blocked %v, want prompt return", elapsed)
	}
}

func TestMusicBrainzAdapter_rateLimit_ctxCancelAbortsWait(t *testing.T) {
	a := NewMusicBrainzAdapter(http.DefaultClient, "test")
	a.limiter.mu.Lock()
	a.limiter.lastReq = time.Now()
	a.limiter.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_ = a.limiter.wait(ctx)
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

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
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

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
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

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
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

func TestMusicBrainzAdapter_ValidateArtistAlbums(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/ws/2/artist"):
			_, _ = w.Write([]byte(`{"artists": [{"id": "mbid-rh", "name": "Radiohead"}]}`))
		case strings.HasPrefix(r.URL.Path, "/ws/2/release-group"):
			_, _ = w.Write([]byte(`{
				"release-group-count": 2,
				"release-groups": [
					` + mbReleaseGroupJSON("rg-1", "OK Computer", "Radiohead", "mbid-rh") + `,
					` + mbReleaseGroupJSON("rg-2", "Kid A", "Radiohead", "mbid-rh") + `
				]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
	albums := []domain.SearchResult{
		{Title: "OK Computer"},
		{Title: "Fake Bootleg 2020"},
	}
	res, err := adapter.ValidateArtistAlbums(context.Background(), "Radiohead", albums)
	if err != nil {
		t.Fatalf("ValidateArtistAlbums: %v", err)
	}
	if res.ArtistMBID != "mbid-rh" {
		t.Errorf("ArtistMBID = %q, want mbid-rh", res.ArtistMBID)
	}
	if len(res.Confirmed) != 1 || res.Confirmed[0].Title != "OK Computer" {
		t.Errorf("Confirmed = %+v, want the MB-matching album only", res.Confirmed)
	}
	if len(res.Unconfirmed) != 1 || res.Unconfirmed[0].Title != "Fake Bootleg 2020" {
		t.Errorf("Unconfirmed = %+v, want the non-matching album", res.Unconfirmed)
	}
}

func TestMusicBrainzAdapter_ValidateArtistAlbums_artistNotFoundIsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"artists": []}`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
	if _, err := adapter.ValidateArtistAlbums(context.Background(), "Nobody", nil); err == nil {
		t.Fatal("expected an error when MB has no artist for the name")
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

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
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

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
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

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
	titles, err := adapter.ReleaseGroupTitles(context.Background(), "")
	if err != nil || titles != nil {
		t.Errorf("ReleaseGroupTitles(\"\") = (%v, %v), want (nil, nil)", titles, err)
	}
}

func TestExtractCreditedMBID_missingCredit(t *testing.T) {
	if got := extractCreditedMBID(mbReleaseGroup{}); got != "" {
		t.Errorf("no artist-credit: got %q, want empty", got)
	}
	rg := mbReleaseGroup{ArtistCredit: []mbArtistRef{{Name: "Che"}}}
	if got := extractCreditedMBID(rg); got != "" {
		t.Errorf("credit without artist link: got %q, want empty", got)
	}
}

func TestMusicBrainzAdapter_Search_malformedJSONIsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`<html>rate limited</html>`))
	}))
	defer server.Close()

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
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

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
	rgs, err := adapter.fetchReleaseGroups(context.Background(), "mbid-1")
	if err != nil {
		t.Fatalf("expected the partial set on a later-page failure, got error: %v", err)
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

	adapter := NewMusicBrainzAdapter(newTestClient(server.URL), "altune-test/1.0")
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
	if r.MBID != "rg-1" {
		t.Errorf("MBID = %q, want rg-1", r.MBID)
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
