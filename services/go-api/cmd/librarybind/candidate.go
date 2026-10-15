package main

import (
	"altune/go-api/internal/shared/releasepick"
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
	ID             string              `json:"id"`
	ISRC           string              `json:"isrc"`
	Title          string              `json:"title"`
	Artists        []string            `json:"artists"`
	Album          string              `json:"album,omitempty"`
	LengthSeconds  float64             `json:"length_seconds"`
	Disambiguation string              `json:"disambiguation,omitempty"`
	Homes          []releasepick.Group `json:"homes,omitempty"`
}

func (c candidate) original() (releasepick.Group, releasepick.Kind, bool) {
	if c.Disambiguation != "" {
		return releasepick.Group{}, releasepick.KindNone, false
	}
	return releasepick.Best(c.Homes, func(g releasepick.Group) releasepick.Group { return g })
}
