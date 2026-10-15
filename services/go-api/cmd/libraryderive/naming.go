package main

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared/songfacts"
	"slices"
	"strings"
)

func titleKeys(title string) domain.SearchKeys {
	return domain.CleanSearchKeys(title, "", "")
}

func sameSongTitle(a, b string) bool {
	keysA, keysB := titleKeys(a), titleKeys(b)
	return songfacts.NameKey(baseTitle(keysA.Title)) == songfacts.NameKey(baseTitle(keysB.Title)) && slices.Equal(keysA.VariantQualifiers(), keysB.VariantQualifiers())
}

func isCredited(stored, credit string) bool {
	wanted := songfacts.NameKey(stored)
	if wanted == "" {
		return false
	}
	if songfacts.NameKey(credit) == wanted {
		return true
	}
	for _, name := range songfacts.SplitCredit(credit) {
		if songfacts.NameKey(name) == wanted {
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
	return slices.Contains(variousArtistsNames, songfacts.NameKey(name))
}

func isSameName(a, b string) bool {
	return songfacts.NameKey(a) != "" && songfacts.NameKey(a) == songfacts.NameKey(b)
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
