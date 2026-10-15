package main

import (
	"altune/go-api/internal/shared/releasepick"
	"altune/go-api/internal/shared/versiontag"
	"slices"
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
	SourceURL   string
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

var releaseTypeNames = []string{strings.ToLower(releasepick.PrimaryAlbum), strings.ToLower(releasepick.PrimarySingle), strings.ToLower(releasepick.PrimaryEP)}

func namesAnotherPerformance(disambiguation string) bool {
	tokens := versiontag.Tokens(disambiguation)
	if slices.ContainsFunc(versiontag.Matches(tokens), func(entry versiontag.Entry) bool { return entry.Veto }) {
		return true
	}
	unexplained := slices.DeleteFunc(versiontag.WithoutVersionPhrases(tokens), func(word string) bool { return slices.Contains(releaseTypeNames, word) })
	return len(unexplained) > 0
}

func (c candidate) original() (releasepick.Group, releasepick.Kind, bool) {
	if namesAnotherPerformance(c.Disambiguation) {
		return releasepick.Group{}, releasepick.KindNone, false
	}
	return releasepick.Best(c.Homes, func(g releasepick.Group) releasepick.Group { return g })
}
