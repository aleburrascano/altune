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
}

func (d difference) category() category {
	if d.Mode == modeReportOnly {
		return category(d.Field + "_differs")
	}
	return category(d.Field + "_" + string(d.Mode))
}

func diff(stored libraryTrack, want desired) []difference {
	moved := albumDiffers(stored.Album, want.Album)
	var found []difference
	found = appendReportOnly(found, "album", stored.Album, want.Album, moved)
	found = appendReportOnly(found, "title", stored.Title, want.Title, normalizeName(stored.Title) != normalizeName(want.Title))
	found = appendReportOnly(found, "artist", stored.Artist, want.Artist, normalizeName(stored.Artist) != normalizeName(want.Artist))
	found = appendWritable(found, "year", intText(stored.Year), intText(want.Year), true)
	found = appendWritable(found, "album_artist", stored.AlbumArtist, want.AlbumArtist, normalizeName(stored.AlbumArtist) != normalizeName(want.AlbumArtist))
	found = appendAlbumBound(found, "track_number", intText(stored.TrackNumber), intText(want.TrackNumber), moved)
	found = appendAlbumBound(found, "genre", stored.Genre, want.Genre, moved)
	found = appendWritable(found, "catalog_duration_seconds", secondsText(stored.CatalogDurationSeconds), secondsText(want.CatalogDurationSeconds),
		math.Abs(stored.CatalogDurationSeconds-want.CatalogDurationSeconds) > catalogLengthSlack)
	return appendAlbumBound(found, "artwork_url", stored.ArtworkURL, want.ArtworkURL, moved)
}

func appendAlbumBound(found []difference, field, stored, want string, albumMoved bool) []difference {
	if albumMoved {
		return appendReportOnly(found, field, stored, want, stored != want)
	}
	return appendWritable(found, field, stored, want, false)
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

func appendWritable(found []difference, field, stored, want string, mayCorrect bool) []difference {
	switch {
	case want == "" || stored == want:
		return found
	case stored == "":
		return append(found, difference{Field: field, Mode: modeFill, New: &want})
	case mayCorrect:
		return append(found, difference{Field: field, Mode: modeCorrect, Old: &stored, New: &want})
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
