package main

import (
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
}

type rule string

const (
	ruleStudioAlbum rule = "studio_album"
	ruleSingleOrEP  rule = "single_or_ep"
	ruleOnlyHome    rule = "only_home"
)

const (
	primaryAlbum  = "Album"
	primarySingle = "Single"
	primaryEP     = "EP"
	yearDigits    = 4
)

type pick struct {
	Group releaseGroup
	Rule  rule
}

func (g releaseGroup) isPlainAlbum() bool {
	return g.PrimaryType == primaryAlbum && len(g.SecondaryTypes) == 0
}

func (g releaseGroup) isPlainSingleOrEP() bool {
	return (g.PrimaryType == primarySingle || g.PrimaryType == primaryEP) && len(g.SecondaryTypes) == 0
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

func isEarlier(a, b string) bool {
	shared := min(len(a), len(b))
	switch {
	case a == "":
		return false
	case b == "":
		return true
	}
	return a[:shared] < b[:shared]
}

func earliest(groups []releaseGroup, qualifies func(releaseGroup) bool) (releaseGroup, bool) {
	var best releaseGroup
	found := false
	for _, g := range groups {
		if qualifies(g) && (!found || isEarlier(g.FirstReleaseDate, best.FirstReleaseDate)) {
			best, found = g, true
		}
	}
	return best, found
}

func chooseRelease(groups []releaseGroup) (pick, bool) {
	if album, ok := earliest(groups, releaseGroup.isPlainAlbum); ok {
		return pick{Group: album, Rule: ruleStudioAlbum}, true
	}
	if single, ok := earliest(groups, releaseGroup.isPlainSingleOrEP); ok {
		return pick{Group: single, Rule: ruleSingleOrEP}, true
	}
	if len(groups) == 1 {
		return pick{Group: groups[0], Rule: ruleOnlyHome}, true
	}
	return pick{}, false
}
