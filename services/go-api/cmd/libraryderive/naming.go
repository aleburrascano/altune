package main

import (
	"altune/go-api/internal/catalog/domain"
	"regexp"
	"slices"
	"strings"
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

const (
	reasonAlbumLacksTrack      = "the deezer tracklist of the chosen album does not list the recording"
	reasonStoredIsOriginal     = "the stored album is an official release group of the recording and the chosen one is not earlier"
	reasonTitleUncorroborated  = "the deezer track does not carry this title"
	reasonArtistUncorroborated = "the catalogs disagree on the artist"
)

func albumHold(storedAlbum string, chosen releaseGroup, homes []releaseGroup, check crossCheck) string {
	if check.AlbumLacksTrack {
		return reasonAlbumLacksTrack
	}
	for _, home := range homes {
		if !albumDiffers(storedAlbum, home.Title) && !isEarlier(chosen.FirstReleaseDate, home.FirstReleaseDate) {
			return reasonStoredIsOriginal
		}
	}
	return ""
}

func titleHold(title string, deezer deezerTrack) string {
	if strings.EqualFold(canonicalText(title), canonicalText(deezer.Title)) {
		return ""
	}
	return reasonTitleUncorroborated
}

func artistHold(artist string, deezer deezerTrack) string {
	if sharesArtist(deezer.Artist, artist) {
		return ""
	}
	return reasonArtistUncorroborated
}
