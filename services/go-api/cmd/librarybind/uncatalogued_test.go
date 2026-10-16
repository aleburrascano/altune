package main

import (
	"errors"
	"testing"
)

func curreTrack() libraryTrack {
	return libraryTrack{
		ID: "t-curre", UserID: "u1", Title: "CURRE, D.J.F - Street Player 2021 (Edit)", Artist: "Curre", FileSeconds: 192.18, Version: 1,
		SourceURL: "https://soundcloud.com/curre-2/chicago-finale-mp3",
	}
}

func answeredWithNothing(reason string) identity {
	return identity{
		Status: statusUnbound, Reason: reason,
		Evidence: evidence{
			ArtistFirst: []sourceOutcome{{Source: sourceDeezer, Candidates: []judged{}}, {Source: sourceApple, Candidates: []judged{}}},
			Search:      []sourceOutcome{{Source: sourceMusicBrainz, Candidates: []judged{}}},
		},
	}
}

func TestMarkUncatalogued_MakesATrackNoCatalogListsUnofficialOnlyWhenItCameFromAUserUploadPlatform(t *testing.T) {
	cases := []struct {
		name       string
		track      libraryTrack
		reason     string
		wantStatus status
		wantKey    string
	}{
		{"a soundcloud upload", curreTrack(), reasonISRCNotFound, statusUnofficial, "soundcloud.com/curre-2/chicago-finale-mp3"},
		{"a soundcloud subdomain", with2(curreTrack(), func(tr *libraryTrack) { tr.SourceURL = "https://m.soundcloud.com/curre-2/x" }), reasonNoISRC, statusUnofficial, "soundcloud.com/curre-2/x"},
		{"a youtube source stays unbound", with2(curreTrack(), func(tr *libraryTrack) { tr.SourceURL = "https://www.youtube.com/watch?v=NifcYqCpA1c" }), reasonNoISRC, statusUnbound, "youtube:NifcYqCpA1c"},
		{"a host that only mentions soundcloud stays unbound", with2(curreTrack(), func(tr *libraryTrack) { tr.SourceURL = "https://notsoundcloud.com/a" }), reasonNoISRC, statusUnbound, "notsoundcloud.com/a"},
		{"a track with no source url stays unbound", with2(curreTrack(), func(tr *libraryTrack) { tr.SourceURL = "" }), reasonNoISRC, statusUnbound, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := markUncatalogued(t.Context(), tc.track, answeredWithNothing(tc.reason))
			if err != nil || got.Status != tc.wantStatus || got.Reason != "not-in-catalog" || got.Evidence.SourceKey != tc.wantKey {
				t.Fatalf("got %s %q key %q, %v", got.Status, got.Reason, got.Evidence.SourceKey, err)
			}
		})
	}
}

func TestMarkUncatalogued_LeavesTheTrackUnboundWhenTheAnswersDoNotProveItIsAbsent(t *testing.T) {
	listsTheTitle := answeredWithNothing(reasonNoISRC)
	listsTheTitle.Evidence.ArtistFirst[1].Candidates = []judged{{candidate: candidate{ID: "a", Title: "CURRE, D.J.F - Street Player 2021 (Edit)", Artists: []string{"Wham!"}}, Failures: []guard{guardArtist}}}
	failed := answeredWithNothing(reasonNoISRC)
	failed.Evidence.Search[0].Error = errors.New("timeout").Error()
	ambiguous := answeredWithNothing(reasonNoISRC)
	ambiguous.Evidence.ArtistFirst[0].Ambiguous = true
	neverSearched := identity{Status: statusUnbound, Reason: reasonNoISRC}
	cases := []struct {
		name  string
		found identity
	}{
		{"a catalog lists the title under another credit", listsTheTitle},
		{"a catalog failed to answer", failed},
		{"a catalog could not tell its candidates apart", ambiguous},
		{"no search ran", neverSearched},
		{"the track was refused rather than unidentified", answeredWithNothing(reasonRefusedPrefix + string(guardTitle))},
		{"the track is already bound", identity{Status: statusBound, Reason: reasonBoundPrefix + "deezer", Evidence: answeredWithNothing("").Evidence}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := markUncatalogued(t.Context(), curreTrack(), tc.found)
			if err != nil || got.Status != tc.found.Status || got.Reason != tc.found.Reason || got.Evidence.SourceKey != "" {
				t.Fatalf("%+v became %+v, %v", tc.found, got, err)
			}
		})
	}
}
