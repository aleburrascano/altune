package main

import (
	"altune/go-api/internal/shared/songfacts"
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
	Title           string
	Artist          string
	Album           string
	Bind            string
	TrackNumber     string
	Genre           string
	ArtworkURL      string
	CatalogDuration string
	ISRC            string
}

func (w withheld) of(field string) string {
	switch field {
	case "title":
		return w.Title
	case "artist":
		return w.Artist
	case "album":
		return w.Album
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
	held := moved && stored.Album != "" && (want.WhyNot.Album != "" || want.WhyNot.Bind != "")
	stance := stanceToward(stored.Album, want.Album, moved, held)
	if held {
		group = ""
	}
	var found []difference
	found = appendHeldOr(found, "album", stored.Album, want.Album, moved, stance, group, want.WhyNot)
	found = appendCorroborated(found, "title", stored.Title, want.Title, !sameSongTitle(stored.Title, want.Title), group, want.WhyNot)
	found = appendCorroborated(found, "artist", stored.Artist, want.Artist, !isCredited(stored.Artist, want.Artist), group, want.WhyNot)
	found = appendAlbumBound(found, "year", intText(stored.Year), intText(want.Year), stance, group, want.WhyNot)
	found = appendHeldOr(found, "album_artist", stored.AlbumArtist, want.AlbumArtist, !isSameName(stored.AlbumArtist, want.AlbumArtist), stance, group, want.WhyNot)
	found = appendAlbumBound(found, "track_number", intText(stored.TrackNumber), intText(want.TrackNumber), stance, group, want.WhyNot)
	found = appendAlbumBound(found, "genre", stored.Genre, want.Genre, stance, group, want.WhyNot)
	found = appendField(found, "catalog_duration_seconds", secondsText(stored.CatalogDurationSeconds), secondsText(want.CatalogDurationSeconds),
		math.Abs(stored.CatalogDurationSeconds-want.CatalogDurationSeconds) > catalogLengthSlack, "", want.WhyNot)
	found = appendField(found, "isrc", stored.ISRC, songfacts.CanonicalISRC(want.ISRC), songfacts.CanonicalISRC(stored.ISRC) != songfacts.CanonicalISRC(want.ISRC), "", want.WhyNot)
	return appendArtwork(found, stored, want, stance, group)
}

type albumStance int

const (
	albumMoves albumStance = iota
	albumSameEdition
	albumOtherEdition
	albumHeld
)

func stanceToward(stored, want string, moved, held bool) albumStance {
	switch {
	case held:
		return albumHeld
	case moved:
		return albumMoves
	case songfacts.NameKey(stored) == songfacts.NameKey(want):
		return albumSameEdition
	}
	return albumOtherEdition
}

func appendArtwork(found []difference, track libraryTrack, want desired, stance albumStance, group string) []difference {
	if track.CoverCheckFailed != "" && track.ArtworkURL != want.ArtworkURL {
		return append(found, unknown("artwork_url", track.ArtworkURL, want.ArtworkURL, track.CoverCheckFailed))
	}
	if track.CoverIsPlaceholder {
		return appendHeldOr(found, "artwork_url", track.ArtworkURL, want.ArtworkURL, true, stance, group, want.WhyNot)
	}
	return appendAlbumBound(found, "artwork_url", track.ArtworkURL, want.ArtworkURL, stance, group, want.WhyNot)
}

func appendAlbumBound(found []difference, field, stored, want string, stance albumStance, group string, whyNot withheld) []difference {
	if stance == albumOtherEdition && want != "" && stored != want {
		return append(found, unknown(field, stored, want, "the stored album is another edition of the chosen release"))
	}
	if stance == albumSameEdition {
		group = ""
	}
	return appendHeldOr(found, field, stored, want, stored != want, stance, group, whyNot)
}

func appendHeldOr(found []difference, field, stored, want string, differs bool, stance albumStance, group string, whyNot withheld) []difference {
	if stance == albumHeld && differs && want != "" {
		return append(found, unknown(field, stored, want, cmp.Or(whyNot.Bind, whyNot.Album)))
	}
	return appendField(found, field, stored, want, differs, group, whyNot)
}

func appendCorroborated(found []difference, field, stored, want string, differs bool, group string, whyNot withheld) []difference {
	if differs && stored != "" && want != "" && whyNot.of(field) != "" {
		return append(found, unknown(field, stored, want, whyNot.of(field)))
	}
	return appendField(found, field, stored, want, differs, group, whyNot)
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
	return want != "" && !sameAlbumGroup(stored, want)
}

func sameAlbumGroup(a, b string) bool {
	return songfacts.NameKey(baseTitle(a)) == songfacts.NameKey(baseTitle(b)) || isColonEdition(a, b) || isColonEdition(b, a)
}

func isColonEdition(edition, group string) bool {
	return group != "" && strings.HasPrefix(strings.ToLower(edition), strings.ToLower(group)+": ")
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
