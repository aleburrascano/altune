package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeRecordings struct {
	fakeCatalog
	searched []recording
	byISRC   map[string][]recording
	err      error
}

func (f fakeRecordings) SearchRecordings(context.Context, string, string) ([]recording, error) {
	return f.searched, f.err
}

func (f fakeRecordings) RecordingsByISRC(_ context.Context, isrc string) ([]recording, error) {
	return f.byISRC[isrc], f.err
}

func deezerFinds(isrc string) fakeCatalog {
	return fakeCatalog{
		search: []catalogTrack{{ID: 2, Title: "Valentina", Artist: "Daniel Caesar", Album: "Freudian", DurationSeconds: 236}},
		byID:   map[int64]catalogTrack{2: {ID: 2, ISRC: isrc}},
	}
}

func valentina(isrcs ...string) recording {
	return recording{ID: "mbid-1", Title: "Valentina", Artists: []string{"Daniel Caesar"}, DurationSeconds: 237, ISRCs: isrcs}
}

func TestAuditTrack_NoISRCGetsAFixWhenDeezerAndMusicBrainzAgree(t *testing.T) {
	lib := libTrack()
	lib.UserID, lib.Version = "u1", 7
	cat := fakeRecordings{fakeCatalog: deezerFinds("USFOUND00001"), searched: []recording{valentina("usfound00001")}}
	got := auditTrack(context.Background(), cat, fakeCovers{}, lib)
	found := findingOf(got.Findings, categoryISRCFillable)
	want := fix{UserID: "u1", Version: 7, Field: "isrc", NewValue: strPtr("USFOUND00001"), Sources: []string{"deezer", "musicbrainz"}}
	if found.Fix == nil || !reflect.DeepEqual(*found.Fix, want) || findingOf(got.Findings, categoryNoISRC).Detail != "" {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestAuditTrack_NoISRCStaysReportOnlyWithTheReasonWhenMusicBrainzDoesNotAgree(t *testing.T) {
	otherSong := recording{ID: "m", Title: "Ransom", Artists: []string{"Lil Tecca"}, DurationSeconds: 236, ISRCs: []string{"USFOUND00001"}}
	wrongLength := recording{ID: "m", Title: "Valentina", Artists: []string{"Daniel Caesar"}, DurationSeconds: 120, ISRCs: []string{"USFOUND00001"}}
	cases := []struct {
		name   string
		cat    fakeRecordings
		reason string
	}{
		{"musicbrainz lists another isrc", fakeRecordings{searched: []recording{valentina("USOTHER00001")}}, "listing USFOUND00001"},
		{"musicbrainz has no recording", fakeRecordings{}, "listing USFOUND00001"},
		{"matching isrc on a different song", fakeRecordings{searched: []recording{otherSong}}, "listing USFOUND00001"},
		{"matching isrc on a recording of the wrong length", fakeRecordings{searched: []recording{wrongLength}}, "listing USFOUND00001"},
		{"musicbrainz lookup fails", fakeRecordings{err: errors.New("boom")}, "musicbrainz lookup failed: boom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.cat.fakeCatalog = deezerFinds("USFOUND00001")
			got := auditTrack(context.Background(), tc.cat, fakeCovers{}, libTrack())
			noISRC, fillable := findingOf(got.Findings, categoryNoISRC), findingOf(got.Findings, categoryISRCFillable)
			if fillable.Fix != nil || fillable.Detail != "USFOUND00001" || !strings.Contains(noISRC.Detail, tc.reason) || got.Blocked != nil {
				t.Fatalf("got %+v", got.Findings)
			}
		})
	}
}

func TestAuditTrack_NoISRCWithoutAUniqueDeezerCandidateNeverReachesAFix(t *testing.T) {
	two := fakeCatalog{search: []catalogTrack{
		{ID: 1, Title: "Valentina", Artist: "Daniel Caesar", Album: "Freudian", DurationSeconds: 236},
		{ID: 2, Title: "Valentina - Remastered", Artist: "Daniel Caesar", Album: "Freudian", DurationSeconds: 238},
	}}
	for name, cat := range map[string]fakeCatalog{"ambiguous": two, "none": {}} {
		got := auditTrack(context.Background(), fakeRecordings{fakeCatalog: cat, searched: []recording{valentina("ISRC1", "ISRC2")}}, fakeCovers{}, libTrack())
		for _, f := range got.Findings {
			if f.Fix != nil {
				t.Fatalf("%s: got a fix %+v", name, f)
			}
		}
	}
}

func TestAuditTrack_NoISRCWithoutAMusicBrainzSourceIsReportOnly(t *testing.T) {
	got := auditTrack(context.Background(), deezerFinds("USFOUND00001"), fakeCovers{}, libTrack())
	if findingOf(got.Findings, categoryISRCFillable).Fix != nil || !strings.Contains(findingOf(got.Findings, categoryNoISRC).Detail, "not consulted") {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestAuditTrack_MusicBrainzBlockIsABlock(t *testing.T) {
	cat := fakeRecordings{fakeCatalog: deezerFinds("USFOUND00001"), err: blocked("musicbrainz 503")}
	if got := auditTrack(context.Background(), cat, fakeCovers{}, libTrack()); !errors.Is(got.Blocked, errCatalogBlocked) {
		t.Fatalf("got %+v", got)
	}
	lib := libTrack()
	lib.ISRC = "NOPE"
	if got := auditTrack(context.Background(), cat, fakeCovers{}, lib); !errors.Is(got.Blocked, errCatalogBlocked) {
		t.Fatalf("unknown isrc: got %+v", got)
	}
}

func TestAuditTrack_UnknownISRCIsKeptWhenMusicBrainzKnowsItForTheSameSong(t *testing.T) {
	lib := libTrack()
	lib.ISRC = "NOPE"
	cat := fakeRecordings{byISRC: map[string][]recording{"NOPE": {valentina("NOPE")}}}
	got := auditTrack(context.Background(), cat, fakeCovers{}, lib)
	if len(got.Findings) != 1 || got.Findings[0].Category != categoryISRCConfirmed || got.Findings[0].Fix != nil {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestAuditTrack_UnknownISRCIsReportedWhenMusicBrainzDoesNotConfirmIt(t *testing.T) {
	otherSong := recording{ID: "m", Title: "Ransom", Artists: []string{"Lil Tecca"}, DurationSeconds: 236}
	cases := map[string]fakeRecordings{
		"musicbrainz does not know it": {},
		"it belongs to another song":   {byISRC: map[string][]recording{"NOPE": {otherSong}}},
		"lookup fails":                 {err: errors.New("boom")},
	}
	for name, cat := range cases {
		t.Run(name, func(t *testing.T) {
			lib := libTrack()
			lib.ISRC = "NOPE"
			got := auditTrack(context.Background(), cat, fakeCovers{}, lib)
			if len(got.Findings) != 1 || got.Findings[0].Category != categoryUnknownISRC || got.Findings[0].Fix != nil {
				t.Fatalf("got %+v", got.Findings)
			}
		})
	}
}

func TestAuditTrack_NoISRCIsFilledFromTheLibraryAlbumNotFromOurDownloadLength(t *testing.T) {
	cat := fakeRecordings{fakeCatalog: deezerFinds("USFOUND00001"), searched: []recording{valentina("USFOUND00001")}}
	wrongDownload := libTrack()
	wrongDownload.DurationSeconds = 283
	if findingOf(auditTrack(context.Background(), cat, fakeCovers{}, wrongDownload).Findings, categoryISRCFillable).Fix == nil {
		t.Error("a same-album candidate was refused because our download has another length")
	}
	otherAlbum := libTrack()
	otherAlbum.Album = "Valentina (Remixes)"
	if findingOf(auditTrack(context.Background(), cat, fakeCovers{}, otherAlbum).Findings, categoryISRCFillable).Fix != nil {
		t.Error("a candidate on another album was filled because its length fits our download")
	}
}

type fakeAlbums struct {
	fakeRecordings
	tracks map[string][]catalogTrack
}

func (f fakeAlbums) AlbumTracks(_ context.Context, _, album string) ([]catalogTrack, error) {
	return f.tracks[album], nil
}

func TestAuditTrack_NoISRCFallsBackToTheLibraryAlbumsTracklist(t *testing.T) {
	lib := libraryTrack{ID: "t1", UserID: "u1", Version: 2, Title: "Diamonds", Artist: "Rihanna", Album: "Unapologetic", DurationSeconds: 283}
	remix := catalogTrack{ID: 9, Title: "Diamonds (Remix)", Artist: "Rihanna", Album: "Diamonds (Remix)", DurationSeconds: 287}
	albumCut := catalogTrack{ID: 62376270, Title: "Diamonds", Artist: "Rihanna", Album: "Unapologetic", DurationSeconds: 225}
	cat := fakeAlbums{
		fakeRecordings: fakeRecordings{
			fakeCatalog: fakeCatalog{search: []catalogTrack{remix}, byID: map[int64]catalogTrack{62376270: {ID: 62376270, ISRC: "USUM71211793"}}},
			searched:    []recording{{ID: "mb", Title: "Diamonds", Artists: []string{"Rihanna"}, DurationSeconds: 225, ISRCs: []string{"USUM71211793"}}},
		},
		tracks: map[string][]catalogTrack{"Unapologetic": {albumCut}},
	}
	found := findingOf(auditTrack(context.Background(), cat, fakeCovers{}, lib).Findings, categoryISRCFillable)
	if found.Fix == nil || *found.Fix.NewValue != "USUM71211793" {
		t.Fatalf("got %+v", found)
	}
}

func TestAuditTrack_CatalogRecordIsTheLibraryAlbumsTrackSharingTheISRC(t *testing.T) {
	lib := libraryTrack{ID: "t1", UserID: "u1", Version: 3, Title: "Poker Face", Artist: "Lady Gaga", Album: "The Fame Monster (Deluxe Edition)", ISRC: "USUM70824409", DurationSeconds: 237}
	compilation := catalogTrack{ID: 1, Title: "Poker Face", Artist: "Lady Gaga", Album: "Papeete 10 Years", ISRC: "USUM70824409", DurationSeconds: 230}
	albumCut := catalogTrack{ID: 2, Title: "Poker Face", Artist: "Lady Gaga", Album: "The Fame Monster (Deluxe Edition)", ISRC: "USUM70824409", DurationSeconds: 237}
	cat := fakeAlbums{
		fakeRecordings: fakeRecordings{fakeCatalog: fakeCatalog{
			byISRC: map[string]catalogTrack{"USUM70824409": compilation},
			byID:   map[int64]catalogTrack{2: albumCut},
		}},
		tracks: map[string][]catalogTrack{"The Fame Monster (Deluxe Edition)": {{ID: 2, Title: "Poker Face", Artist: "Lady Gaga", Album: "The Fame Monster (Deluxe Edition)", DurationSeconds: 237}}},
	}
	got := auditTrack(context.Background(), cat, fakeCovers{}, lib)
	if got.Matched == nil || got.Matched.ID != 2 || got.Matched.DurationSeconds != 237 {
		t.Fatalf("got %+v", got.Matched)
	}
}

func TestAuditTrack_CatalogRecordFallsBackToTheISRCLookupWhenTheAlbumHasNoSuchTrack(t *testing.T) {
	lib := libraryTrack{ID: "t1", Title: "Poker Face", Artist: "Lady Gaga", Album: "The Fame Monster (Deluxe Edition)", ISRC: "USUM70824409", DurationSeconds: 230}
	compilation := catalogTrack{ID: 1, Title: "Poker Face", Artist: "Lady Gaga", Album: "Papeete 10 Years", ISRC: "USUM70824409", DurationSeconds: 230}
	cat := fakeAlbums{
		fakeRecordings: fakeRecordings{fakeCatalog: fakeCatalog{byISRC: map[string]catalogTrack{"USUM70824409": compilation}}},
		tracks:         map[string][]catalogTrack{"The Fame Monster (Deluxe Edition)": {{ID: 5, Title: "Bad Romance", Artist: "Lady Gaga", Album: "The Fame Monster (Deluxe Edition)"}}},
	}
	got := auditTrack(context.Background(), cat, fakeCovers{}, lib)
	if got.Matched == nil || got.Matched.ID != 1 {
		t.Fatalf("got %+v", got.Matched)
	}
}

func pokerFaceLibrary(savedCatalogSeconds float64) libraryTrack {
	return libraryTrack{ID: "t1", UserID: "u1", Version: 3, Title: "Poker Face", Artist: "Lady Gaga", Album: "The Fame Monster (Deluxe Edition)", ISRC: "USUM70824409", DurationSeconds: 237, CatalogDurationSeconds: savedCatalogSeconds}
}

func pokerFaceCatalog(withAlbumTrack bool) fakeAlbums {
	const album = "The Fame Monster (Deluxe Edition)"
	compilation := catalogTrack{ID: 1, Title: "Poker Face", Artist: "Lady Gaga", Album: "Papeete 10 Years", ISRC: "USUM70824409", DurationSeconds: 230}
	albumCut := catalogTrack{ID: 107876612, Title: "Poker Face", Artist: "Lady Gaga", Album: album, ISRC: "USUM70824409", DurationSeconds: 237}
	cat := fakeAlbums{
		fakeRecordings: fakeRecordings{fakeCatalog: fakeCatalog{byISRC: map[string]catalogTrack{"USUM70824409": compilation}, byID: map[int64]catalogTrack{107876612: albumCut}}},
		tracks:         map[string][]catalogTrack{album: {{ID: 5, Title: "Bad Romance", Artist: "Lady Gaga", Album: album}}},
	}
	if withAlbumTrack {
		cat.tracks[album] = []catalogTrack{{ID: 107876612, Title: "Poker Face", Artist: "Lady Gaga", Album: album, DurationSeconds: 237}}
	}
	return cat
}

func TestAuditTrackWithFills_AlbumTrackLengthCorrectsADifferingSavedCatalogLength(t *testing.T) {
	got := auditTrackWithFills(context.Background(), pokerFaceCatalog(true), fakeCovers{}, pokerFaceLibrary(230))
	found := findingOf(got.Findings, categoryCatalogDurationCorrectable)
	want := fix{UserID: "u1", Version: 3, Field: "catalog_duration_seconds", Mode: "correct", OldValue: strPtr("230"), NewValue: strPtr("237")}
	if found.Fix == nil || !reflect.DeepEqual(*found.Fix, want) {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestAuditTrackWithFills_ISRCLookupLengthNeverCorrectsASavedCatalogLength(t *testing.T) {
	got := auditTrackWithFills(context.Background(), pokerFaceCatalog(false), fakeCovers{}, pokerFaceLibrary(237))
	if found := findingOf(got.Findings, categoryCatalogDurationCorrectable); found.Category != "" {
		t.Fatalf("got %+v", found)
	}
}

func TestAuditTrackWithFills_AnAlbumTrackLengthEqualToTheSavedCatalogLengthYieldsNothing(t *testing.T) {
	got := auditTrackWithFills(context.Background(), pokerFaceCatalog(true), fakeCovers{}, pokerFaceLibrary(237))
	for _, f := range got.Findings {
		if strings.HasPrefix(string(f.Category), "catalog_duration") {
			t.Fatalf("got %+v", f)
		}
	}
}
