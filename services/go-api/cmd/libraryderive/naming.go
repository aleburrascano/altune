package main

import (
	"altune/go-api/internal/catalog/domain"
	"regexp"
	"slices"
)

var creditSeparatorRe = regexp.MustCompile(`(?i)\s*(?:&|\+|,|;|/|\bfeat\.?|\bft\.?|\bfeaturing\b|\bwith\b|\bx\b|\band\b)\s*`)

func titleKeys(title string) domain.SearchKeys {
	return domain.CleanSearchKeys(title, "", "")
}

func sameSongTitle(a, b string) bool {
	keysA, keysB := titleKeys(a), titleKeys(b)
	return normalizeName(baseTitle(keysA.Title)) == normalizeName(baseTitle(keysB.Title)) && slices.Equal(keysA.VariantQualifiers(), keysB.VariantQualifiers())
}

func isCredited(stored, credit string) bool {
	wanted := normalizeName(stored)
	if wanted == "" {
		return false
	}
	if normalizeName(credit) == wanted {
		return true
	}
	for _, name := range creditSeparatorRe.Split(credit, -1) {
		if normalizeName(name) == wanted {
			return true
		}
	}
	return false
}

func ownSingles(groups []releaseGroup, songTitle string) []releaseGroup {
	return slices.DeleteFunc(slices.Clone(groups), func(g releaseGroup) bool {
		return g.PrimaryType == primarySingle && !sameSongTitle(g.Title, songTitle)
	})
}

var variousArtistsNames = []string{"variousartists", "va", "variosartistas", "verschiedeneinterpreten"}

func isVariousArtists(name string) bool {
	return slices.Contains(variousArtistsNames, normalizeName(name))
}

func isSameName(a, b string) bool {
	return normalizeName(a) != "" && normalizeName(a) == normalizeName(b)
}
