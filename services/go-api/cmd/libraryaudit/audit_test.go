package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

type fakeCatalog struct {
	byISRC   map[string]catalogTrack
	failISRC map[string]error
	search   []catalogTrack
	byID     map[int64]catalogTrack
}

func (f fakeCatalog) TrackByISRC(_ context.Context, isrc string) (catalogTrack, error) {
	if err := f.failISRC[isrc]; err != nil {
		return catalogTrack{}, err
	}
	found, ok := f.byISRC[isrc]
	if !ok {
		return catalogTrack{}, errCatalogUnknown
	}
	return found, nil
}

func (f fakeCatalog) Search(context.Context, string, string) ([]catalogTrack, error) {
	return f.search, nil
}

func (f fakeCatalog) TrackByID(_ context.Context, id int64) (catalogTrack, error) {
	return f.byID[id], nil
}

type fakeCovers struct {
	same bool
	err  error
}

func (f fakeCovers) SameImage(context.Context, string, string) (bool, error) { return f.same, f.err }

func audited(cat fakeCatalog, covers fakeCovers, lib libraryTrack) trackResult {
	return auditTrack(context.Background(), cat, covers, lib)
}

func withCover(cat catalogTrack) catalogTrack {
	cat.CoverURL = "http://dz/cover.jpg"
	cat.AlbumImageMD5 = "md5"
	return cat
}

func TestAuditTrack_UnknownIsrcIsReportedNotChecked(t *testing.T) {
	lib := libTrack()
	lib.ISRC = "NOPE"
	got := audited(fakeCatalog{}, fakeCovers{}, lib)
	if got.Checked || len(got.Findings) != 1 || got.Findings[0].Category != categoryUnknownISRC {
		t.Fatalf("got %+v", got)
	}
}

func TestAuditTrack_LookupFailureIsReportedNotChecked(t *testing.T) {
	lib := libTrack()
	lib.ISRC = "X"
	got := audited(fakeCatalog{failISRC: map[string]error{"X": errors.New("boom")}}, fakeCovers{}, lib)
	if got.Checked || got.Findings[0].Category != categoryLookupFailed {
		t.Fatalf("got %+v", got)
	}
}

func TestAuditTrack_NoIsrcFindsIsrcFromSearchWhenSongAgrees(t *testing.T) {
	lib := libTrack()
	cat := fakeCatalog{
		search: []catalogTrack{{ID: 1, Title: "Other", Artist: "Someone"}, {ID: 2, Title: "Valentina", Artist: "Daniel Caesar", Album: "Freudian", DurationSeconds: 236}},
		byID:   map[int64]catalogTrack{2: {ID: 2, ISRC: "USFOUND"}},
	}
	got := audited(cat, fakeCovers{}, lib)
	if got.Checked || len(got.Findings) != 2 ||
		got.Findings[0].Category != categoryNoISRC ||
		got.Findings[1].Category != categoryISRCFillable || got.Findings[1].Detail != "USFOUND" {
		t.Fatalf("got %+v", got)
	}
}

func TestAuditTrack_NoIsrcAndNoSearchMatchIsOnlyNoIsrc(t *testing.T) {
	got := audited(fakeCatalog{search: []catalogTrack{{ID: 1, Title: "Other", Artist: "Someone"}}}, fakeCovers{}, libTrack())
	if len(got.Findings) != 1 || got.Findings[0].Category != categoryNoISRC {
		t.Fatalf("got %+v", got)
	}
}

func TestAuditTrack_WrongSongSkipsCoverComparison(t *testing.T) {
	lib := libTrack()
	lib.ISRC, lib.ArtworkURL = "X", "http://lib/a.jpg"
	cat := fakeCatalog{byISRC: map[string]catalogTrack{"X": withCover(catalogTrack{Title: "Ransom", Artist: "Lil Tecca"})}}
	got := audited(cat, fakeCovers{same: false}, lib)
	if !got.Checked || len(got.Findings) != 1 || got.Findings[0].Category != categoryWrongSong {
		t.Fatalf("got %+v", got)
	}
}

func TestAuditTrack_CoverComparison(t *testing.T) {
	cases := []struct {
		name    string
		artwork string
		covers  fakeCovers
		want    []category
	}{
		{"same image from another edition", "http://lib/a.jpg", fakeCovers{same: true}, []category{categoryCoverURLDiffers}},
		{"different image", "http://lib/a.jpg", fakeCovers{same: false}, []category{categoryCoverURLDiffers, categoryCoverImageDiffers}},
		{"unreadable image", "http://lib/a.jpg", fakeCovers{err: errors.New("bad")}, []category{categoryCoverURLDiffers, categoryCoverUnreadable}},
		{"url carries the album image id", "http://cdn/cover/md5/250.jpg", fakeCovers{same: false}, nil},
		{"track has no artwork", "", fakeCovers{same: false}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lib := libTrack()
			lib.ISRC, lib.ArtworkURL = "X", tc.artwork
			cat := fakeCatalog{byISRC: map[string]catalogTrack{"X": withCover(catTrack())}}
			got := categoriesOf(audited(cat, tc.covers, lib).Findings)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v want %v", got, tc.want)
				}
			}
		})
	}
}

func TestAuditAll_CountsCheckedAndKeepsEveryFinding(t *testing.T) {
	good, unknown, none := libTrack(), libTrack(), libTrack()
	good.ISRC, unknown.ISRC, none.ISRC = "OK", "NOPE", ""
	cat := fakeCatalog{byISRC: map[string]catalogTrack{"OK": catTrack()}}
	got := auditAll(context.Background(), cat, fakeCovers{}, []libraryTrack{good, unknown, none}, 2)
	s := summarize(got)
	if s.ReadyTracks != 3 || s.Checked != 1 || s.UnknownISRC != 1 || s.NoISRC != 1 {
		t.Fatalf("got %+v", s)
	}
}

type blockedCatalog struct {
	fakeCatalog
	calls atomic.Int32
}

func (b *blockedCatalog) TrackByISRC(context.Context, string) (catalogTrack, error) {
	b.calls.Add(1)
	return catalogTrack{}, blocked("HTTP 403")
}

func TestAuditAll_StopsAtTheFirstBlockAndKeepsThePartialResult(t *testing.T) {
	tracks := make([]libraryTrack, 200)
	for i := range tracks {
		tracks[i] = libTrack()
		tracks[i].ISRC = "X"
	}
	cat := &blockedCatalog{}
	got := auditAll(context.Background(), cat, fakeCovers{}, tracks, 2)
	if !strings.Contains(got.AbortReason, "HTTP 403") {
		t.Fatalf("abort reason %q", got.AbortReason)
	}
	if cat.calls.Load() > 4 {
		t.Fatalf("kept calling the catalog after a block: %d calls", cat.calls.Load())
	}
	if summarize(got).LookupFailed != 0 {
		t.Fatalf("a block must not be counted as a per-track failure: %+v", summarize(got))
	}
}

func TestAuditTrack_BlockDuringSearchIsABlockNotAMissingMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer server.Close()
	got := auditTrack(context.Background(), newDeezerClient(server.Client(), server.URL, 1000), fakeCovers{}, libTrack())
	if !errors.Is(got.Blocked, errCatalogBlocked) {
		t.Fatalf("got %+v", got)
	}
}

func TestAuditTrack_NoIsrcResolvesThroughDeezerSearchWithUniqueMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/2.0/search":
			_, _ = w.Write([]byte(`{"data":[
				{"id":1,"title":"Valentina Remix Pack","artist":{"name":"Other Artist"}},
				{"id":2,"title":"Valentina","duration":236,"artist":{"name":"Daniel Caesar"},"album":{"title":"Freudian"}}]}`))
		case "/2.0/track/2":
			_, _ = w.Write([]byte(`{"id":2,"title":"Valentina","isrc":"CAUNIQUE","artist":{"name":"Daniel Caesar"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	got := auditTrack(context.Background(), newDeezerClient(server.Client(), server.URL, 1000), fakeCovers{}, libTrack())
	if len(got.Findings) != 2 || got.Findings[1].Category != categoryISRCFillable || got.Findings[1].Detail != "CAUNIQUE" {
		t.Fatalf("got %+v", got)
	}
}

func searchFor(candidates ...catalogTrack) fakeCatalog {
	byID := map[int64]catalogTrack{}
	for _, candidate := range candidates {
		byID[candidate.ID] = catalogTrack{ID: candidate.ID, ISRC: "ISRC" + strconv.FormatInt(candidate.ID, 10)}
	}
	return fakeCatalog{search: candidates, byID: byID}
}

func TestAuditTrack_SearchPicksTheOneCandidateOnTheLibraryAlbum(t *testing.T) {
	cat := searchFor(
		catalogTrack{ID: 1, Title: "Valentina", Artist: "Daniel Caesar", Album: "Valentina (Remixes)", DurationSeconds: 236},
		catalogTrack{ID: 2, Title: "Valentina", Artist: "Daniel Caesar", Album: "Freudian", DurationSeconds: 210},
	)
	got := audited(cat, fakeCovers{}, libTrack())
	if len(got.Findings) != 2 || got.Findings[1].Category != categoryISRCFillable || got.Findings[1].Detail != "ISRC2" {
		t.Fatalf("got %+v", got)
	}
}

func TestAuditTrack_SearchWithTwoFittingCandidatesIsAmbiguousAndFillsNothing(t *testing.T) {
	cat := searchFor(
		catalogTrack{ID: 1, Title: "Valentina", Artist: "Daniel Caesar", Album: "Freudian", DurationSeconds: 236},
		catalogTrack{ID: 2, Title: "Valentina - Remastered", Artist: "Daniel Caesar", Album: "Freudian", DurationSeconds: 238},
	)
	got := audited(cat, fakeCovers{}, libTrack())
	if len(got.Findings) != 2 || got.Findings[1].Category != categoryISRCAmbiguous {
		t.Fatalf("got %+v", got)
	}
	if hasCategory(got.Findings, categoryISRCFillable) {
		t.Fatalf("an ambiguous match must not fill an ISRC: %+v", got)
	}
}

func TestAuditTrack_SearchWithNoCandidateOnTheLibraryAlbumFillsNothing(t *testing.T) {
	cat := searchFor(catalogTrack{ID: 1, Title: "Valentina", Artist: "Daniel Caesar", Album: "Valentina (Remixes)", DurationSeconds: 236})
	got := audited(cat, fakeCovers{}, libTrack())
	if len(got.Findings) != 1 || got.Findings[0].Category != categoryNoISRC {
		t.Fatalf("got %+v", got)
	}
}

func strPtr(s string) *string { return &s }

func findingOf(findings []finding, want category) finding {
	for _, f := range findings {
		if f.Category == want {
			return f
		}
	}
	return finding{}
}

func withLib(lib libraryTrack, edit func(*libraryTrack)) libraryTrack {
	edit(&lib)
	return lib
}

func TestAuditTrack_FindingsCarryTheExactFixAndTheVersionSeen(t *testing.T) {
	base := libTrack()
	base.UserID, base.Version = "u1", 7
	cases := []struct {
		name     string
		category category
		lib      libraryTrack
		cat      fakeCatalog
		want     fix
	}{
		{
			"year", categoryYearFillable, withLib(base, func(l *libraryTrack) { l.ISRC, l.Year = "X", 0 }),
			fakeCatalog{byISRC: map[string]catalogTrack{"X": catTrack()}},
			fix{UserID: "u1", Version: 7, Field: "year", NewValue: strPtr("2019")},
		},
		{
			"album artist", categoryAlbumArtistFillable, withLib(base, func(l *libraryTrack) { l.ISRC, l.AlbumArtist = "X", "" }),
			fakeCatalog{byISRC: map[string]catalogTrack{"X": catTrack()}},
			fix{UserID: "u1", Version: 7, Field: "album_artist", NewValue: strPtr("Daniel Caesar")},
		},
		{
			"wrong song clears isrc", categoryWrongSong, withLib(base, func(l *libraryTrack) { l.ISRC = "X" }),
			fakeCatalog{byISRC: map[string]catalogTrack{"X": {Title: "Ransom", Artist: "Lil Tecca"}}},
			fix{UserID: "u1", Version: 7, Field: "isrc", OldValue: strPtr("X")},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := audited(tc.cat, fakeCovers{}, tc.lib)
			found := findingOf(got.Findings, tc.category)
			if found.Fix == nil || !reflect.DeepEqual(*found.Fix, tc.want) {
				t.Fatalf("got %+v", found)
			}
		})
	}
}
