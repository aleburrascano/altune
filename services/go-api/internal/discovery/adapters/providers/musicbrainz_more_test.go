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

// --- rateLimit --------------------------------------------------------------

// Each concurrent caller must reserve a DISTINCT future slot (lastReq advances
// by 1s per caller under the lock) — the pre-fix form let concurrent callers
// share a baseline and burst together into MB 503s. Reservation happens under
// the lock regardless of the sleep, so a cancelled ctx observes it instantly.
func TestMusicBrainzAdapter_rateLimit_reservesDistinctSlots(t *testing.T) {
	a := NewMusicBrainzAdapter(http.DefaultClient, "test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // skip the sleeps; only the slot arithmetic is under test

	start := time.Now()
	a.rateLimit(ctx)
	a.rateLimit(ctx)
	a.rateLimit(ctx)

	a.mu.Lock()
	last := a.lastReq
	a.mu.Unlock()

	// First call lands on now; each subsequent call reserves +1s.
	if got := last.Sub(start); got < 1900*time.Millisecond || got > 3*time.Second {
		t.Errorf("lastReq advanced by %v, want ~2s (three callers spaced 1s apart)", got)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("cancelled-ctx callers blocked %v, want prompt return", elapsed)
	}
}

func TestMusicBrainzAdapter_rateLimit_ctxCancelAbortsWait(t *testing.T) {
	a := NewMusicBrainzAdapter(http.DefaultClient, "test")
	a.mu.Lock()
	a.lastReq = time.Now() // next slot is 1s away
	a.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	a.rateLimit(ctx)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("rateLimit blocked %v after ctx cancel, want prompt return", elapsed)
	}
}

// --- structured query -------------------------------------------------------

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
		default: // artist kind fails
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
	// Unicode (Turkish ö, emoji, CJK) must survive the parse/map path verbatim.
	if results[0].Title != "Söz 🎵 東京" {
		t.Errorf("title = %q, want the unicode title preserved", results[0].Title)
	}
}

// --- artist identity --------------------------------------------------------

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

	// Second call must come from the memo — no extra MB round trip.
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
		{"196", 0},     // too short
		{"19x9-01", 0}, // non-digit
	}
	for _, tt := range tests {
		if got := parseBirthYear(tt.in); got != tt.want {
			t.Errorf("parseBirthYear(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// --- validate / discography -------------------------------------------------

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
	if r.Extras["record_type"] != "album" {
		t.Errorf("record_type = %v, want %q (lower-cased primary-type)", r.Extras["record_type"], "album")
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
	rg := mbReleaseGroup{ArtistCredit: []mbArtistRef{{Name: "Che"}}} // credit without artist link
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
