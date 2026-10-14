package main

import (
	"strings"
	"testing"
)

const luckiISRC = "QZXYZ2400001"

func luckiDeezer(recordType string) *fakeDeezer {
	return &fakeDeezer{
		byISRC: map[string]deezerTrack{
			luckiISRC: {ID: "5", ISRC: luckiISRC, Title: "Witchcraft", Artist: "LUCKI", Position: 3, DurationSeconds: 150, AlbumID: "55", AlbumTitle: "Alone in the Dark"},
		},
		albums: map[string]deezerAlbum{
			"55": {ID: "55", Title: "Alone in the Dark", CoverURL: "https://cdn/lucki.jpg", Genre: "Rap", RecordType: recordType, ReleaseDate: "2023-05-12", Artist: "LUCKI"},
		},
	}
}

func luckiTrack() boundTrack {
	return boundTrack{
		libraryTrack: libraryTrack{ID: "t9", UserID: "u1", Title: "Witchcraft", Artist: "LUCKI", ISRC: luckiISRC, Version: 3},
		IdentityISRC: luckiISRC,
	}
}

func TestDeriveAll_WithoutAMusicBrainzRecordingTheDeezerAlbumOfTheIsrcIsTheRelease(t *testing.T) {
	for _, recordType := range []string{"album", "ep", "single"} {
		t.Run(recordType, func(t *testing.T) {
			got := newDeriver(&fakeMusicBrainz{}, luckiDeezer(recordType)).deriveAll(t.Context(), []boundTrack{luckiTrack()})
			byKind := byCategory(got.Findings)
			year := byKind["year_fill"]
			album, position := byKind["album_fill"], byKind["track_number_fill"]
			if !strings.Contains(byKind[categoryReleaseChosen].Detail, "Alone in the Dark") || year.Fix == nil || *year.Fix.NewValue != "2023" ||
				album.Fix == nil || album.Fix.Mode != modeFill || *album.Fix.NewValue != "Alone in the Dark" ||
				position.Fix == nil || position.Fix.Mode != modeFill || *position.Fix.NewValue != "3" ||
				byKind["album_differs"].Category != "" || byKind["track_number_differs"].Category != "" {
				t.Fatalf("got %+v", got.Findings)
			}
			for _, f := range got.Findings {
				if f.Source != "deezer" {
					t.Errorf("finding %s has source %q", f.Category, f.Source)
				}
			}
		})
	}
}

func TestDeriveAll_ADeezerCompilationIsNeverTheRelease(t *testing.T) {
	got := newDeriver(&fakeMusicBrainz{}, luckiDeezer("compile")).deriveAll(t.Context(), []boundTrack{luckiTrack()})
	if len(got.Findings) != 1 || got.Findings[0].Category != categoryReleaseUnchosen || got.Findings[0].Fix != nil {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestDeriveAll_ADeezerTrackWithAnotherIsrcLeavesTheRecordingUnknown(t *testing.T) {
	dz := luckiDeezer("album")
	dz.byISRC[luckiISRC] = deezerTrack{ID: "5", ISRC: "QZXYZ2499999", Title: "Witchcraft", AlbumID: "55"}
	got := newDeriver(&fakeMusicBrainz{}, dz).deriveAll(t.Context(), []boundTrack{luckiTrack()})
	if len(got.Findings) != 1 || got.Findings[0].Category != categoryRecordingUnknown {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestDeriveAll_NeitherCatalogKnowingTheIsrcStaysRecordingUnknown(t *testing.T) {
	got := newDeriver(&fakeMusicBrainz{}, &fakeDeezer{}).deriveAll(t.Context(), []boundTrack{luckiTrack()})
	if len(got.Findings) != 1 || got.Findings[0].Category != categoryRecordingUnknown {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestDeriveAll_ADeezerAlbumByVariousArtistsOrAnotherArtistIsNeverTheRelease(t *testing.T) {
	cases := map[string]string{
		"Various Artists": "Various Artists", "VA": "VA", "Varios Arti" + "stas": "Varios Arti" + "stas",
		"Verschiedene Interpreten": "Verschiedene Interpreten", "another artist": "Pitch Perfect Cast",
	}
	for name, albumArtist := range cases {
		t.Run(name, func(t *testing.T) {
			dz := luckiDeezer("album")
			album := dz.albums["55"]
			album.Artist = albumArtist
			dz.albums["55"] = album
			got := newDeriver(&fakeMusicBrainz{}, dz).deriveAll(t.Context(), []boundTrack{luckiTrack()})
			if len(got.Findings) != 1 || got.Findings[0].Category != categoryReleaseUnchosen || got.Findings[0].Fix != nil {
				t.Fatalf("got %+v", got.Findings)
			}
		})
	}
}

func TestDeriveAll_TheDeezerOnlyPathExplainsEveryStoredFieldItCannotDerive(t *testing.T) {
	dz := luckiDeezer("album")
	album := dz.albums["55"]
	album.CoverURL = ""
	dz.albums["55"] = album
	track := dz.byISRC[luckiISRC]
	track.DurationSeconds = 0
	dz.byISRC[luckiISRC] = track
	stored := luckiTrack()
	stored.ArtworkURL, stored.CatalogDurationSeconds = "https://cdn/old.jpg", 151
	byKind := byCategory(newDeriver(&fakeMusicBrainz{}, dz).deriveAll(t.Context(), []boundTrack{stored}).Findings)
	for kind, reason := range map[category]string{
		"artwork_url_differs":              "no usable cover",
		"catalog_duration_seconds_differs": "no track length",
	} {
		if f := byKind[kind]; f.Fix != nil || f.Source != sourceDeezer || !strings.Contains(f.Detail, reason) {
			t.Errorf("%s: %+v", kind, f)
		}
	}
}
