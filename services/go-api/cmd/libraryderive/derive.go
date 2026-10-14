package main

import (
	"math"
	"strconv"
)

type anchor struct {
	ISRC   string
	Title  string
	Artist string
}

type desired struct {
	Title                  string
	Artist                 string
	Album                  string
	AlbumArtist            string
	Year                   int
	TrackNumber            int
	Genre                  string
	CatalogDurationSeconds float64
	ArtworkURL             string
	ISRC                   string
}

func derive(a anchor, chosen releaseGroup, match *deezerMatch) desired {
	want := desired{
		Title: a.Title, Artist: a.Artist, Album: chosen.Title, AlbumArtist: chosen.Artist,
		Year: chosen.year(), ISRC: a.ISRC,
	}
	if match != nil {
		want.TrackNumber, want.Genre = match.TrackPosition, match.Genre
		want.CatalogDurationSeconds, want.ArtworkURL = match.DurationSeconds, match.CoverURL
	}
	return want
}

type libraryTrack struct {
	ID                     string
	UserID                 string
	Title                  string
	Artist                 string
	Album                  string
	AlbumArtist            string
	ISRC                   string
	ArtworkURL             string
	Year                   int
	Genre                  string
	TrackNumber            int
	CatalogDurationSeconds float64
	FileSeconds            float64
	Version                int
}

type boundTrack struct {
	libraryTrack
	IdentityISRC  string
	MBRecordingID string
}

type changeMode string

const (
	modeFill       changeMode = "fill"
	modeCorrect    changeMode = "correct"
	modeReportOnly changeMode = "report"
)

type difference struct {
	Field string
	Mode  changeMode
	Old   *string
	New   *string
	Group string
}

func (d difference) category() category {
	if d.Mode == modeReportOnly {
		return category(d.Field + "_differs")
	}
	return category(d.Field + "_" + string(d.Mode))
}

func diff(stored libraryTrack, want desired) []difference {
	moved := albumDiffers(stored.Album, want.Album)
	group := ""
	if moved && stored.Album != "" {
		group = "album-move:" + stored.ID
	}
	var found []difference
	found = appendIdentity(found, "album", stored.Album, want.Album, moved, true, group)
	found = appendIdentity(found, "title", stored.Title, want.Title, !sameSongTitle(stored.Title, want.Title), sameVariant(stored.Title, want.Title), group)
	found = appendIdentity(found, "artist", stored.Artist, want.Artist, !isCredited(stored.Artist, want.Artist), true, group)
	found = appendWritable(found, "year", intText(stored.Year), intText(want.Year), true, group)
	found = appendWritable(found, "album_artist", stored.AlbumArtist, want.AlbumArtist, !isSameName(stored.AlbumArtist, want.AlbumArtist), group)
	found = appendAlbumBound(found, "track_number", intText(stored.TrackNumber), intText(want.TrackNumber), moved, group)
	found = appendAlbumBound(found, "genre", stored.Genre, want.Genre, moved, group)
	found = appendWritable(found, "catalog_duration_seconds", secondsText(stored.CatalogDurationSeconds), secondsText(want.CatalogDurationSeconds),
		math.Abs(stored.CatalogDurationSeconds-want.CatalogDurationSeconds) > catalogLengthSlack, "")
	return appendArtwork(found, stored.ArtworkURL, want.ArtworkURL, moved, group)
}

func appendArtwork(found []difference, stored, want string, albumMoved bool, group string) []difference {
	if stored == "" {
		return appendReportOnly(found, "artwork_url", stored, want, true)
	}
	return appendAlbumBound(found, "artwork_url", stored, want, albumMoved, group)
}

func (d difference) isIdentity() bool {
	return d.Field == "album" || d.Field == "title" || d.Field == "artist"
}

func (d difference) reportOnly() difference {
	d.Mode, d.Group = modeReportOnly, ""
	return d
}

func appendIdentity(found []difference, field, stored, want string, differs, safeToWrite bool, group string) []difference {
	if want == "" || !differs {
		return found
	}
	if stored == "" || !safeToWrite {
		return append(found, difference{Field: field, Mode: modeReportOnly, Old: optional(stored), New: &want})
	}
	return append(found, difference{Field: field, Mode: modeCorrect, Old: &stored, New: &want, Group: group})
}

func appendAlbumBound(found []difference, field, stored, want string, albumMoved bool, group string) []difference {
	switch {
	case !albumMoved:
		return appendWritable(found, field, stored, want, false, "")
	case group == "":
		return appendReportOnly(found, field, stored, want, stored != want)
	}
	return appendWritable(found, field, stored, want, true, group)
}

func albumDiffers(stored, want string) bool {
	return want != "" && normalizeName(baseTitle(stored)) != normalizeName(baseTitle(want))
}

func appendReportOnly(found []difference, field, stored, want string, differs bool) []difference {
	if want == "" || !differs {
		return found
	}
	return append(found, difference{Field: field, Mode: modeReportOnly, Old: optional(stored), New: &want})
}

func appendWritable(found []difference, field, stored, want string, mayCorrect bool, group string) []difference {
	switch {
	case want == "" || stored == want:
		return found
	case stored == "":
		return append(found, difference{Field: field, Mode: modeFill, New: &want, Group: group})
	case mayCorrect:
		return append(found, difference{Field: field, Mode: modeCorrect, Old: &stored, New: &want, Group: group})
	}
	return found
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func intText(value int) string {
	if value <= 0 {
		return ""
	}
	return strconv.Itoa(value)
}

func secondsText(value float64) string {
	if value <= 0 {
		return ""
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}
