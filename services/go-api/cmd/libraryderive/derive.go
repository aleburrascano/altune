package main

import (
	"altune/go-api/internal/shared/textnorm"
	"cmp"
	"math"
	"strconv"
	"strings"
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
	WhyNot                 withheld
}

func derive(a anchor, chosen releaseGroup, match *deezerMatch) desired {
	want := desired{
		Title: canonicalText(a.Title), Artist: canonicalText(a.Artist), Album: canonicalText(chosen.Title), AlbumArtist: chosen.Artist,
		Year: chosen.year(), ISRC: a.ISRC,
	}
	if match != nil {
		want.TrackNumber, want.Genre = match.TrackPosition, match.Genre
		want.CatalogDurationSeconds, want.ArtworkURL, want.WhyNot = match.DurationSeconds, match.CoverURL, match.Withheld
	}
	return want
}

func canonicalText(value string) string {
	return strings.Join(strings.Fields(textnorm.NFKC(value)), " ")
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
	CoverIsPlaceholder     bool
	CoverCheckFailed       string
}

type boundTrack struct {
	libraryTrack
	IdentityISRC     string
	CatalogConfirmed bool
	MBRecordingID    string
}

type changeMode string

const (
	modeFill       changeMode = "fill"
	modeCorrect    changeMode = "correct"
	modeReportOnly changeMode = "report"
)

type difference struct {
	Field  string
	Mode   changeMode
	Old    *string
	New    *string
	Group  string
	Reason string
}

func (d difference) category() category {
	if d.Mode == modeReportOnly {
		return category(d.Field + "_differs")
	}
	return category(d.Field + "_" + string(d.Mode))
}

type withheld struct {
	TrackNumber     string
	Genre           string
	ArtworkURL      string
	CatalogDuration string
	ISRC            string
}

func (w withheld) of(field string) string {
	switch field {
	case "track_number":
		return w.TrackNumber
	case "genre":
		return w.Genre
	case "artwork_url":
		return w.ArtworkURL
	case "catalog_duration_seconds":
		return w.CatalogDuration
	case "isrc":
		return w.ISRC
	}
	return ""
}

const reasonNoValue = "no trustworthy value on the chosen release"

func diff(stored libraryTrack, want desired) []difference {
	moved := albumDiffers(stored.Album, want.Album)
	group := ""
	if moved && stored.Album != "" {
		group = "album-move:" + stored.ID
	}
	stance := stanceToward(stored.Album, want.Album, moved)
	var found []difference
	found = appendField(found, "album", stored.Album, want.Album, moved, group, want.WhyNot)
	found = appendField(found, "title", stored.Title, want.Title, !sameSongTitle(stored.Title, want.Title), group, want.WhyNot)
	found = appendField(found, "artist", stored.Artist, want.Artist, !isCredited(stored.Artist, want.Artist), group, want.WhyNot)
	found = appendField(found, "year", intText(stored.Year), intText(want.Year), stored.Year != want.Year, group, want.WhyNot)
	found = appendField(found, "album_artist", stored.AlbumArtist, want.AlbumArtist, !isSameName(stored.AlbumArtist, want.AlbumArtist), group, want.WhyNot)
	found = appendAlbumBound(found, "track_number", intText(stored.TrackNumber), intText(want.TrackNumber), stance, group, want.WhyNot)
	found = appendAlbumBound(found, "genre", stored.Genre, want.Genre, stance, group, want.WhyNot)
	found = appendField(found, "catalog_duration_seconds", secondsText(stored.CatalogDurationSeconds), secondsText(want.CatalogDurationSeconds),
		math.Abs(stored.CatalogDurationSeconds-want.CatalogDurationSeconds) > catalogLengthSlack, "", want.WhyNot)
	found = appendField(found, "isrc", stored.ISRC, canonicalISRC(want.ISRC), canonicalISRC(stored.ISRC) != canonicalISRC(want.ISRC), "", want.WhyNot)
	return appendArtwork(found, stored, want, stance, group)
}

type albumStance int

const (
	albumMoves albumStance = iota
	albumSameEdition
	albumOtherEdition
)

func stanceToward(stored, want string, moved bool) albumStance {
	switch {
	case moved:
		return albumMoves
	case normalizeName(stored) == normalizeName(want):
		return albumSameEdition
	}
	return albumOtherEdition
}

func appendArtwork(found []difference, track libraryTrack, want desired, stance albumStance, group string) []difference {
	if track.CoverCheckFailed != "" && track.ArtworkURL != want.ArtworkURL {
		return append(found, unknown("artwork_url", track.ArtworkURL, want.ArtworkURL, track.CoverCheckFailed))
	}
	if track.CoverIsPlaceholder {
		return appendField(found, "artwork_url", track.ArtworkURL, want.ArtworkURL, true, group, want.WhyNot)
	}
	return appendAlbumBound(found, "artwork_url", track.ArtworkURL, want.ArtworkURL, stance, group, want.WhyNot)
}

func appendAlbumBound(found []difference, field, stored, want string, stance albumStance, group string, whyNot withheld) []difference {
	if stance == albumOtherEdition && stored != "" && want != "" && stored != want {
		return append(found, unknown(field, stored, want, "the stored album is another edition of the chosen release"))
	}
	if stance == albumSameEdition {
		group = ""
	}
	return appendField(found, field, stored, want, stored != want, group, whyNot)
}

func appendField(found []difference, field, stored, want string, differs bool, group string, whyNot withheld) []difference {
	switch {
	case want == "" && stored == "":
		return found
	case want == "":
		return append(found, unknown(field, stored, want, cmp.Or(whyNot.of(field), reasonNoValue)))
	case !differs || stored == want:
		return found
	case stored == "":
		return append(found, difference{Field: field, Mode: modeFill, New: &want, Group: group})
	}
	return append(found, difference{Field: field, Mode: modeCorrect, Old: &stored, New: &want, Group: group})
}

func unknown(field, stored, want, reason string) difference {
	return difference{Field: field, Mode: modeReportOnly, Old: optional(stored), New: optional(want), Reason: reason}
}

func albumDiffers(stored, want string) bool {
	return want != "" && normalizeName(baseTitle(stored)) != normalizeName(baseTitle(want))
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
