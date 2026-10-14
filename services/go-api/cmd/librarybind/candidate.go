package main

import (
	"strings"
)

type source string

const (
	sourceDeezer      source = "deezer"
	sourceMusicBrainz source = "musicbrainz"
	sourceApple       source = "apple"
)

type libraryTrack struct {
	ID          string
	UserID      string
	Title       string
	Artist      string
	Album       string
	ISRC        string
	FileSeconds float64
	Version     int
}

func (t libraryTrack) hasUnreleasedAlbum() bool {
	return strings.EqualFold(strings.TrimSpace(t.Album), "UNRELEASED")
}

type candidate struct {
	ID            string   `json:"id"`
	ISRC          string   `json:"isrc"`
	Title         string   `json:"title"`
	Artists       []string `json:"artists"`
	Album         string   `json:"album,omitempty"`
	LengthSeconds float64  `json:"length_seconds"`
}

func canonicalISRC(isrc string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(isrc), "-", ""))
}
