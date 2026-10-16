package main

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/shared/textnorm"
	"strings"
)

type target struct {
	TrackID        string
	UserID         string
	Artist         string
	Title          string
	Album          string
	ISRC           string
	CatalogSeconds float64
	Version        int
}

type rejection string

const (
	accepted           rejection = ""
	rejectNotArtTrack  rejection = "not-art-track"
	rejectArtist       rejection = "artist-not-credited"
	rejectTitle        rejection = "title-differs"
	rejectNoCatalogLen rejection = "no-catalog-length"
	rejectLength       rejection = "length-differs"
	rejectAlbum        rejection = "other-album"
	rejectVideoID      rejection = "malformed-video-id"
)

const unreleasedAlbum = "unreleased"

func (t target) judge(song ytmsearch.Song) rejection {
	switch {
	case song.Type != ytmsearch.VideoTypeATV:
		return rejectNotArtTrack
	case !acqdomain.IsYouTubeVideoID(song.VideoID):
		return rejectVideoID
	case !ytmsearch.CreditsArtist(song.Artists, t.Artist):
		return rejectArtist
	case !ytmsearch.SameTitle(song.Title, t.Title):
		return rejectTitle
	case t.CatalogSeconds <= 0:
		return rejectNoCatalogLen
	case !acqdomain.LengthAuthoritative.Agrees(t.CatalogSeconds, float64(song.DurationSeconds)):
		return rejectLength
	case t.knowsAlbum() && !t.isOnAlbum(song):
		return rejectAlbum
	}
	return accepted
}

func (t target) knowsAlbum() bool {
	album := strings.ToLower(strings.TrimSpace(t.Album))
	return album != "" && album != unreleasedAlbum
}

func (t target) isOnAlbum(song ytmsearch.Song) bool {
	return textnorm.NormalizeForMatch(song.Album) == textnorm.NormalizeForMatch(t.Album)
}
