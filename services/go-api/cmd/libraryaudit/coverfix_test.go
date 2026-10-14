package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeBlankCovers struct {
	fakeCovers
	blank map[string]bool
	err   error
}

func (f fakeBlankCovers) IsBlank(_ context.Context, url string) (bool, error) {
	return f.blank[url], f.err
}

type fakeAlbumKinds struct {
	fakeEnriching
	kinds map[int64]string
	err   error
}

func (f fakeAlbumKinds) AlbumRecordType(_ context.Context, albumID int64) (string, error) {
	return f.kinds[albumID], f.err
}

const (
	libraryCover = "https://lib/cover.jpg"
	catalogCover = "https://cdn/cover/xl.jpg"
)

func coverLibrary() libraryTrack {
	lib := libraryOnFreudian()
	lib.ArtworkURL = libraryCover
	return lib
}

func coverCatalogTrack() catalogTrack {
	found := freudianTrack()
	found.CoverURL, found.FullCoverURL, found.AlbumImageMD5 = "https://cdn/cover/medium.jpg", catalogCover, "md5"
	return found
}

func coverAudit(found catalogTrack, kinds fakeAlbumKinds, covers coverJudge, lib libraryTrack) trackResult {
	kinds.byISRC = map[string]catalogTrack{"X": found}
	if kinds.kinds == nil {
		kinds.kinds = map[int64]string{9: "album"}
	}
	return auditTrackWithFills(context.Background(), kinds, covers, lib)
}

func TestAuditTrack_ReplacesAWrongCoverWithTheCoverOfTheTracksOwnAlbum(t *testing.T) {
	for _, recordType := range []string{"album", "ep"} {
		got := coverAudit(coverCatalogTrack(), fakeAlbumKinds{kinds: map[int64]string{9: recordType}}, fakeBlankCovers{}, coverLibrary())
		differs := findingOf(got.Findings, categoryCoverImageDiffers)
		if differs.Fix == nil || differs.Fix.Field != "artwork_url" || *differs.Fix.OldValue != libraryCover || *differs.Fix.NewValue != catalogCover ||
			differs.Fix.UserID != "u1" || differs.Fix.Version != 7 || hasCategory(got.Findings, categoryCoverNotReplaceable) {
			t.Fatalf("%s: got %+v", recordType, got.Findings)
		}
	}
}

func TestAuditTrack_NeverReplacesACoverFromASingleCompilationOrAnotherAlbum(t *testing.T) {
	otherAlbum := coverCatalogTrack()
	otherAlbum.Album = "Valentina - Single"
	noCover := coverCatalogTrack()
	noCover.FullCoverURL = ""
	cases := []struct {
		name   string
		found  catalogTrack
		kinds  fakeAlbumKinds
		reason string
	}{
		{"single", coverCatalogTrack(), fakeAlbumKinds{kinds: map[int64]string{9: "single"}}, `"single"`},
		{"compilation", coverCatalogTrack(), fakeAlbumKinds{kinds: map[int64]string{9: "compile"}}, `"compile"`},
		{"unknown type", coverCatalogTrack(), fakeAlbumKinds{kinds: map[int64]string{9: ""}}, `""`},
		{"another album", otherAlbum, fakeAlbumKinds{}, "is not the library album"},
		{"no catalog cover", noCover, fakeAlbumKinds{}, "no album id or cover"},
		{"type lookup fails", coverCatalogTrack(), fakeAlbumKinds{err: errors.New("boom")}, "album type lookup failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := coverAudit(tc.found, tc.kinds, fakeBlankCovers{}, coverLibrary())
			skipped := findingOf(got.Findings, categoryCoverNotReplaceable)
			if findingOf(got.Findings, categoryCoverImageDiffers).Fix != nil || !strings.Contains(skipped.Detail, tc.reason) || got.Blocked != nil {
				t.Fatalf("got %+v", got.Findings)
			}
		})
	}
}

func TestAuditTrack_DoesNotReplaceACoverWithABlankCatalogCover(t *testing.T) {
	covers := fakeBlankCovers{blank: map[string]bool{"https://cdn/cover/medium.jpg": true}}
	got := coverAudit(coverCatalogTrack(), fakeAlbumKinds{}, covers, coverLibrary())
	if findingOf(got.Findings, categoryCoverImageDiffers).Fix != nil || !hasCategory(got.Findings, categoryCoverNotReplaceable) {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestAuditTrack_SameImageOnAnotherEditionIsNotReplaced(t *testing.T) {
	got := coverAudit(coverCatalogTrack(), fakeAlbumKinds{}, fakeBlankCovers{fakeCovers: fakeCovers{same: true}}, coverLibrary())
	if hasCategory(got.Findings, categoryCoverImageDiffers) || hasCategory(got.Findings, categoryCoverNotReplaceable) {
		t.Fatalf("got %+v", got.Findings)
	}
	for _, f := range got.Findings {
		if f.Fix != nil && f.Fix.Field == "artwork_url" {
			t.Fatalf("got %+v", got.Findings)
		}
	}
}

func TestAuditTrack_NoCoverFixWithoutAnAlbumTypeSource(t *testing.T) {
	cat := fakeEnriching{fakeCatalog: fakeCatalog{byISRC: map[string]catalogTrack{"X": coverCatalogTrack()}}}
	got := auditTrackWithFills(context.Background(), cat, fakeBlankCovers{}, coverLibrary())
	if findingOf(got.Findings, categoryCoverImageDiffers).Fix != nil || !strings.Contains(findingOf(got.Findings, categoryCoverNotReplaceable).Detail, "not consulted") {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestAuditTrack_FlagsABlankLibraryCoverEvenWhenItsUrlNamesTheAlbumImage(t *testing.T) {
	lib := coverLibrary()
	lib.ArtworkURL = "https://cdn/cover/md5/250.jpg"
	covers := fakeBlankCovers{blank: map[string]bool{lib.ArtworkURL: true}}
	got := coverAudit(coverCatalogTrack(), fakeAlbumKinds{}, covers, lib)
	if blank := findingOf(got.Findings, categoryCoverBlank); blank.Detail != lib.ArtworkURL || blank.Fix != nil {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestAuditTrack_ARealCoverIsNotFlaggedBlankAndAnImageErrorIsNotABlank(t *testing.T) {
	for name, covers := range map[string]fakeBlankCovers{"real": {}, "unreadable": {err: errors.New("bad")}} {
		got := coverAudit(coverCatalogTrack(), fakeAlbumKinds{}, covers, coverLibrary())
		if hasCategory(got.Findings, categoryCoverBlank) {
			t.Fatalf("%s: got %+v", name, got.Findings)
		}
	}
}

func TestAuditTrack_AnAlbumTypeBlockIsABlock(t *testing.T) {
	got := coverAudit(coverCatalogTrack(), fakeAlbumKinds{err: blocked("deezer 403")}, fakeBlankCovers{}, coverLibrary())
	if !errors.Is(got.Blocked, errCatalogBlocked) {
		t.Fatalf("got %+v", got)
	}
}
