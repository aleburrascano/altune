package main

import (
	"altune/go-api/internal/shared/songfacts"
	"cmp"
	"context"
	"errors"
)

const sourceDeezer = "deezer"

var deezerRecordTypes = map[string]string{"album": primaryAlbum, "ep": primaryEP, "single": primarySingle}

func (d *deriver) deezerOnlyFindings(ctx context.Context, t boundTrack, unknown skipped) ([]finding, error) {
	isrc := cmp.Or(t.IdentityISRC, t.ISRC)
	if isrc == "" {
		return nil, unknown
	}
	track, album, err := d.deezerAlbumOfISRC(ctx, isrc)
	if err != nil && !errors.Is(err, errCatalogUnknown) {
		return nil, err
	}
	if err != nil || album.RecordType == "" {
		return nil, unknown
	}
	if d.verdictOf(ctx, album.CoverURL) != coverGenuine {
		album.CoverURL = ""
	}
	return deezerReleaseFindings(t, isrc, track, album)
}

func deezerReleaseFindings(t boundTrack, isrc string, track deezerTrack, album deezerAlbum) ([]finding, error) {
	primary, accepted := deezerRecordTypes[album.RecordType]
	if !accepted {
		return nil, skipped{categoryReleaseUnchosen, "deezer album " + album.Title + " is a " + album.RecordType + ", not an album, ep or single"}
	}
	if isVariousArtists(album.Artist) || !sharesArtist(track.Artist, album.Artist) {
		return nil, skipped{categoryReleaseUnchosen, "deezer album " + album.Title + " is by " + album.Artist + ", not the track's artist"}
	}
	group := releaseGroup{ID: "deezer:" + album.ID, Title: album.Title, Artist: album.Artist, PrimaryType: primary, FirstReleaseDate: album.ReleaseDate}
	a := anchor{ISRC: isrc, Title: track.Title, Artist: track.Artist}
	head := about(t, categoryReleaseChosen, group.describe()+" by deezer_album; deezer "+string(outcomeSameISRC))
	found := append([]finding{head}, differenceFindings(t, a, group, deezerOnlyMatch(album, track))...)
	for i := range found {
		found[i].Source = sourceDeezer
	}
	return found, nil
}

func deezerOnlyMatch(album deezerAlbum, track deezerTrack) *deezerMatch {
	match := newMatch(album, track)
	if match.CoverURL == "" {
		match.Withheld.ArtworkURL = "deezer has no usable cover for the album (missing, the blank placeholder or not verifiable) and there is no musicbrainz release to fall back on"
	}
	if match.DurationSeconds == 0 {
		match.Withheld.CatalogDuration = "deezer has no track length for the track"
	}
	return match
}

func (d *deriver) deezerAlbumOfISRC(ctx context.Context, isrc string) (deezerTrack, deezerAlbum, error) {
	track, err := d.checker.deezer.TrackByISRC(ctx, isrc)
	if err != nil {
		return deezerTrack{}, deezerAlbum{}, err
	}
	if songfacts.CanonicalISRC(track.ISRC) != songfacts.CanonicalISRC(isrc) {
		return deezerTrack{}, deezerAlbum{}, errCatalogUnknown
	}
	album, err := d.checker.deezer.Album(ctx, track.AlbumID)
	return track, album, err
}

func sharesArtist(a, b string) bool {
	return isCredited(a, b) || isCredited(b, a)
}
