package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeEnriching struct {
	fakeCatalog
	genres    map[int64][]string
	genresErr error
	recs      []recording
	recsErr   error
}

func (f fakeEnriching) AlbumGenres(_ context.Context, albumID int64) ([]string, error) {
	return f.genres[albumID], f.genresErr
}

func (f fakeEnriching) RecordingsWithReleases(context.Context, string) ([]recording, error) {
	return f.recs, f.recsErr
}

func libraryOnFreudian() libraryTrack {
	lib := libTrack()
	lib.UserID, lib.Version = "u1", 7
	lib.ISRC, lib.Album = "X", "Freudian"
	return lib
}

func freudianTrack() catalogTrack {
	found := catTrack()
	found.Album, found.AlbumID, found.TrackPosition, found.DiskNumber = "Freudian", 9, 4, 1
	return found
}

func enrichedBy(found catalogTrack, edit func(*fakeEnriching), lib libraryTrack) trackResult {
	cat := fakeEnriching{fakeCatalog: fakeCatalog{byISRC: map[string]catalogTrack{"X": found}}, genres: map[int64][]string{9: {"R&B"}}}
	if edit != nil {
		edit(&cat)
	}
	return auditTrackWithFills(context.Background(), cat, fakeCovers{}, lib)
}

func TestAuditTrack_FillsTrackNumberGenreAndCatalogLengthFromTheTracksOwnAlbum(t *testing.T) {
	got := enrichedBy(freudianTrack(), nil, libraryOnFreudian())
	wants := map[category]fix{
		categoryTrackNumberFillable:     {UserID: "u1", Version: 7, Field: "track_number", NewValue: strPtr("4")},
		categoryGenreFillable:           {UserID: "u1", Version: 7, Field: "genre", NewValue: strPtr("R&B")},
		categoryCatalogDurationFillable: {UserID: "u1", Version: 7, Field: "catalog_duration_seconds", NewValue: strPtr("236")},
	}
	for kind, want := range wants {
		if found := findingOf(got.Findings, kind); found.Fix == nil || !reflect.DeepEqual(*found.Fix, want) {
			t.Fatalf("%s: got %+v", kind, got.Findings)
		}
	}
}

func TestAuditTrack_LeavesFilledFieldsAlone(t *testing.T) {
	lib := libraryOnFreudian()
	lib.TrackNumber, lib.Genre, lib.CatalogDurationSeconds = 2, "Pop", 240
	if got := enrichedBy(freudianTrack(), nil, lib); len(got.Findings) != 0 {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestAuditTrack_TrackNumberAndGenreComeOnlyFromTheLibraryAlbum(t *testing.T) {
	single := freudianTrack()
	single.Album = "Valentina - Single"
	got := enrichedBy(single, nil, libraryOnFreudian())
	for _, kind := range []category{categoryTrackNumberUnfillable, categoryGenreUnfillable} {
		if found := findingOf(got.Findings, kind); found.Fix != nil || found.Category != kind {
			t.Fatalf("%s: got %+v", kind, got.Findings)
		}
	}
	if hasCategory(got.Findings, categoryTrackNumberFillable) || hasCategory(got.Findings, categoryGenreFillable) {
		t.Fatalf("a single filled album fields: %+v", got.Findings)
	}
	if !hasCategory(got.Findings, categoryCatalogDurationFillable) {
		t.Fatalf("length is per recording and fills from any release: %+v", got.Findings)
	}
}

func TestAuditTrack_TrackNumberOnALaterDiscIsNotFilled(t *testing.T) {
	found := freudianTrack()
	found.DiskNumber = 2
	got := enrichedBy(found, nil, libraryOnFreudian())
	if unfilled := findingOf(got.Findings, categoryTrackNumberUnfillable); !strings.Contains(unfilled.Detail, "disc 2") || hasCategory(got.Findings, categoryTrackNumberFillable) {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestAuditTrack_LibraryWithoutAnAlbumGetsNoTrackNumberOrGenre(t *testing.T) {
	lib := libraryOnFreudian()
	lib.Album = ""
	found := freudianTrack()
	found.Album = ""
	got := enrichedBy(found, nil, lib)
	if hasCategory(got.Findings, categoryTrackNumberFillable) || hasCategory(got.Findings, categoryGenreFillable) {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestAuditTrack_GenreIsReportOnlyUnlessTheAlbumListsExactlyOne(t *testing.T) {
	cases := map[string]func(*fakeEnriching){
		"two genres":    func(f *fakeEnriching) { f.genres = map[int64][]string{9: {"R&B", "Pop"}} },
		"no genres":     func(f *fakeEnriching) { f.genres = map[int64][]string{9: {}} },
		"lookup failed": func(f *fakeEnriching) { f.genresErr = errors.New("boom") },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			got := enrichedBy(freudianTrack(), edit, libraryOnFreudian())
			if hasCategory(got.Findings, categoryGenreFillable) || !hasCategory(got.Findings, categoryGenreUnfillable) || got.Blocked != nil {
				t.Fatalf("got %+v", got.Findings)
			}
		})
	}
}

func TestAuditTrack_AGenreLookupBlockIsABlock(t *testing.T) {
	got := enrichedBy(freudianTrack(), func(f *fakeEnriching) { f.genresErr = blocked("deezer 403") }, libraryOnFreudian())
	if !errors.Is(got.Blocked, errCatalogBlocked) {
		t.Fatalf("got %+v", got)
	}
}

func otherReleaseLibrary() libraryTrack {
	lib := libraryOnFreudian()
	lib.Year, lib.AlbumArtist = 0, ""
	return lib
}

func singleRelease() catalogTrack {
	found := freudianTrack()
	found.Album = "Valentina - Single"
	return found
}

func recordingOn(releases ...release) []recording {
	return []recording{{ID: "rec-1", Title: "Valentina", Artists: []string{"Daniel Caesar"}, Releases: releases}}
}

func TestAuditTrack_YearAndAlbumArtistComeFromTheOneReleaseTitledLikeTheLibraryAlbum(t *testing.T) {
	releases := recordingOn(
		release{ID: "r-single", Title: "Valentina", Date: "2017-01-01", Artist: "Daniel Caesar"},
		release{ID: "r-album", Title: "freudian", Date: "2017-08-25", Artist: "Daniel Caesar feat. Kali Uchis"},
	)
	got := enrichedBy(singleRelease(), func(f *fakeEnriching) { f.recs = releases }, otherReleaseLibrary())
	year, artist := findingOf(got.Findings, categoryYearFromRelease), findingOf(got.Findings, categoryAlbumArtistFromRelease)
	if year.Fix == nil || *year.Fix.NewValue != "2017" || year.Fix.Field != "year" ||
		artist.Fix == nil || *artist.Fix.NewValue != "Daniel Caesar feat. Kali Uchis" || artist.Fix.Field != "album_artist" {
		t.Fatalf("got %+v", got.Findings)
	}
	if hasCategory(got.Findings, categoryYearOtherRelease) {
		t.Fatalf("the resolved finding must replace the report-only one: %+v", got.Findings)
	}
}

func TestAuditTrack_OtherReleaseStaysReportOnlyWithoutExactlyOneMatchingRelease(t *testing.T) {
	album := release{ID: "r1", Title: "Freudian", Date: "2017-08-25", Artist: "Daniel Caesar"}
	cases := map[string]func(*fakeEnriching){
		"no release has the title": func(f *fakeEnriching) { f.recs = recordingOn(release{ID: "r0", Title: "Other", Date: "2017"}) },
		"two releases have it": func(f *fakeEnriching) {
			f.recs = recordingOn(album, release{ID: "r2", Title: "Freudian", Date: "2018-01-01", Artist: "Daniel Caesar"})
		},
		"the release has no date": func(f *fakeEnriching) {
			f.recs = recordingOn(release{ID: "r1", Title: "Freudian", Artist: "Daniel Caesar"})
		},
		"the recording is another song": func(f *fakeEnriching) {
			f.recs = []recording{{ID: "rec-2", Title: "Ransom", Artists: []string{"Lil Tecca"}, Releases: []release{album}}}
		},
		"lookup failed": func(f *fakeEnriching) { f.recsErr = errors.New("boom") },
		"no recording":  func(f *fakeEnriching) {},
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			got := enrichedBy(singleRelease(), edit, otherReleaseLibrary())
			if hasCategory(got.Findings, categoryYearFromRelease) || !hasCategory(got.Findings, categoryYearOtherRelease) ||
				findingOf(got.Findings, categoryYearOtherRelease).Fix != nil || got.Blocked != nil {
				t.Fatalf("got %+v", got.Findings)
			}
		})
	}
}

func TestAuditTrack_SameReleaseTwiceInTheRecordingCountsOnce(t *testing.T) {
	album := release{ID: "r1", Title: "Freudian", Date: "2017-08-25", Artist: "Daniel Caesar"}
	got := enrichedBy(singleRelease(), func(f *fakeEnriching) { f.recs = append(recordingOn(album), recordingOn(album)...) }, otherReleaseLibrary())
	if !hasCategory(got.Findings, categoryYearFromRelease) {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestAuditTrack_AMusicBrainzBlockWhileResolvingReleasesIsABlock(t *testing.T) {
	got := enrichedBy(singleRelease(), func(f *fakeEnriching) { f.recsErr = blocked("musicbrainz 503") }, otherReleaseLibrary())
	if !errors.Is(got.Blocked, errCatalogBlocked) {
		t.Fatalf("got %+v", got)
	}
}
