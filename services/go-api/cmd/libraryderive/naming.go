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
	reasonAlbumLacksTrack         = "the deezer tracklist of the chosen album does not list the recording"
	reasonStoredIsOriginal        = "the stored album is an official release group of the recording and the chosen one is not earlier"
	reasonStoredListsTrackEarlier = "deezer lists the track on the stored album, which is earlier than the chosen release"
	reasonTitleUncorroborated     = "the deezer track does not carry this title"
	reasonArtistUncorroborated    = "the catalogs disagree on the artist"
	reasonAlbumArtistDisputed     = "the deezer album is credited to another artist than the chosen release"
	reasonChosenIsCompilation     = "the chosen release is a compilation, soundtrack, dj-mix or various-artists release and an album is stored"
	reasonChosenIsAnotherArtists  = "the chosen release is credited to another artist than the recording's primary artist"
	reasonStoredTiesChosenYear    = "deezer lists the track on the stored album, which shares the only year the chosen release dates"
)

var compilationTypes = []string{"Compilation", "Soundtrack", "DJ-mix"}

func (g releaseGroup) isCompilationLike() bool {
	return isVariousArtists(g.Artist) || slices.ContainsFunc(g.SecondaryTypes, func(kind string) bool { return slices.Contains(compilationTypes, kind) })
}

func (g releaseGroup) credits(rec recording) bool {
	if rec.PrimaryID != "" && slices.Contains(g.ArtistIDs, rec.PrimaryID) {
		return true
	}
	lead := songfacts.NameKey(rec.leadArtist())
	return slices.ContainsFunc(g.ArtistNames, func(name string) bool { return songfacts.NameKey(name) == lead })
}

func noDeezerAlbum(release string) string {
	return "no deezer album matches " + release
}

func albumArtistHold(chosenArtist string, match *deezerMatch, release string) string {
	switch {
	case match == nil:
		return noDeezerAlbum(release)
	case sharesArtist(match.AlbumArtist, chosenArtist):
		return ""
	}
	return reasonAlbumArtistDisputed
}

func albumHold(storedAlbum string, chosen releaseGroup, homes []releaseGroup, check crossCheck, leadArtist string) string {
	switch {
	case check.AlbumLacksTrack:
		return reasonAlbumLacksTrack
	case storedAlbum != "" && chosen.isCompilationLike():
		return reasonChosenIsCompilation
	case storedAlbum != "" && leadArtist != "" && chosen.Artist != "" && !isCredited(leadArtist, chosen.Artist):
		return reasonChosenIsAnotherArtists
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

func bindHold(artist string, deezer deezerTrack) string {
	if deezer.Artist == "" {
		return ""
	}
	return artistHold(artist, deezer)
}
