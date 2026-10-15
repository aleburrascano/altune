package main

import (
	"altune/go-api/internal/shared/releasepick"
	"strconv"
	"strings"
)

type releaseGroup struct {
	ID               string
	Title            string
	Artist           string
	PrimaryType      string
	SecondaryTypes   []string
	FirstReleaseDate string
	ReleaseID        string
	FrontReleaseID   string
	ArtistIDs        []string
	ArtistNames      []string
}

type rule string

const (
	ruleStudioAlbum rule = "studio_album"
	ruleMixtape     rule = "mixtape"
	ruleSingleOrEP  rule = "single_or_ep"
	ruleOnlyHome    rule = "only_home"
	ruleStoredAlbum rule = "stored_album"
)

const (
	primaryAlbum  = releasepick.PrimaryAlbum
	primarySingle = releasepick.PrimarySingle
	primaryEP     = releasepick.PrimaryEP
	yearDigits    = 4
)

type pick struct {
	Group releaseGroup
	Rule  rule
}

func (g releaseGroup) pickable() releasepick.Group {
	return releasepick.Group{PrimaryType: g.PrimaryType, SecondaryTypes: g.SecondaryTypes, Date: g.FirstReleaseDate}
}

func (g releaseGroup) year() int {
	if len(g.FirstReleaseDate) < yearDigits {
		return 0
	}
	year, err := strconv.Atoi(g.FirstReleaseDate[:yearDigits])
	if err != nil {
		return 0
	}
	return year
}

func (g releaseGroup) describe() string {
	kind := g.PrimaryType
	if len(g.SecondaryTypes) > 0 {
		kind += "+" + strings.Join(g.SecondaryTypes, "+")
	}
	return g.Title + " (" + kind + ", " + g.FirstReleaseDate + ")"
}

var ruleOfKind = map[releasepick.Kind]rule{
	releasepick.KindStudioAlbum: ruleStudioAlbum,
	releasepick.KindMixtape:     ruleMixtape,
	releasepick.KindSingleOrEP:  ruleSingleOrEP,
}

func chooseRelease(groups []releaseGroup) (pick, bool) {
	if best, kind, ok := releasepick.Best(groups, releaseGroup.pickable); ok {
		return pick{Group: best, Rule: ruleOfKind[kind]}, true
	}
	if len(groups) == 1 {
		return pick{Group: groups[0], Rule: ruleOnlyHome}, true
	}
	return pick{}, false
}
