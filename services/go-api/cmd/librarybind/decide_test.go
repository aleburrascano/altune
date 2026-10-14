package main

import (
	"errors"
	"testing"
)

const celebrationISRC = "USPR18000001"

func celebration() libraryTrack {
	return libraryTrack{
		ID: "track-1", UserID: "user-1", Title: "Celebration", Artist: "Kool & The Gang",
		Album: "Celebrate!", ISRC: celebrationISRC, FileSeconds: 298, Version: 4,
	}
}

func catalogCelebration(id string) candidate {
	return candidate{ID: id, ISRC: celebrationISRC, Title: "Celebration", Artists: []string{"Kool & The Gang"}, LengthSeconds: 298}
}

func with(c candidate, change func(*candidate)) candidate {
	change(&c)
	return c
}

func TestDecide_BindRule(t *testing.T) {
	cases := []struct {
		name       string
		track      libraryTrack
		outcomes   []sourceOutcome
		wantStatus status
		wantReason string
	}{
		{
			name:       "second source resolves the isrc at the same length",
			track:      celebration(),
			outcomes:   []sourceOutcome{outcomeOf(celebration(), sourceDeezer, []candidate{catalogCelebration("77")}, nil)},
			wantStatus: statusBound,
			wantReason: "isrc confirmed by deezer",
		},
		{
			name:  "length within 3s still binds",
			track: celebration(),
			outcomes: []sourceOutcome{outcomeOf(celebration(), sourceDeezer, []candidate{
				with(catalogCelebration("77"), func(c *candidate) { c.LengthSeconds = 295.5 }),
			}, nil)},
			wantStatus: statusBound,
			wantReason: "isrc confirmed by deezer",
		},
		{
			name:  "length 5s off refuses the bind",
			track: celebration(),
			outcomes: []sourceOutcome{outcomeOf(celebration(), sourceDeezer, []candidate{
				with(catalogCelebration("77"), func(c *candidate) { c.LengthSeconds = 221 }),
			}, nil)},
			wantStatus: statusUnbound,
			wantReason: "refused-length",
		},
		{
			name:  "same length and title but a different artist does not bind",
			track: celebration(),
			outcomes: []sourceOutcome{outcomeOf(celebration(), sourceDeezer, []candidate{
				with(catalogCelebration("77"), func(c *candidate) { c.Artists = []string{"Jimmy Eat World"} }),
			}, nil)},
			wantStatus: statusUnbound,
			wantReason: "refused-artist",
		},
		{
			name:  "a remix never binds to the original",
			track: celebration(),
			outcomes: []sourceOutcome{outcomeOf(celebration(), sourceApple, []candidate{
				with(catalogCelebration("9"), func(c *candidate) { c.Title = "Celebration (Remix)" }),
			}, nil)},
			wantStatus: statusUnbound,
			wantReason: "refused-title",
		},
		{
			name:  "an original never binds to a remix track",
			track: libraryTrack{ID: "t", Title: "Celebration (Slowed)", Artist: "Kool & The Gang", ISRC: celebrationISRC, FileSeconds: 298},
			outcomes: []sourceOutcome{outcomeOf(libraryTrack{Title: "Celebration (Slowed)", Artist: "Kool & The Gang", ISRC: celebrationISRC, FileSeconds: 298},
				sourceDeezer, []candidate{catalogCelebration("77")}, nil)},
			wantStatus: statusUnbound,
			wantReason: "refused-title",
		},
		{
			name:  "a soundtrack label in brackets is not a version qualifier",
			track: libraryTrack{ID: "t", Title: "Sunflower", Artist: "Post Malone", ISRC: celebrationISRC, FileSeconds: 158},
			outcomes: []sourceOutcome{outcomeOf(libraryTrack{Title: "Sunflower", Artist: "Post Malone", ISRC: celebrationISRC, FileSeconds: 158},
				sourceDeezer, []candidate{{
					ID: "9", ISRC: celebrationISRC, Title: "Sunflower (Spider-Man: Into the Spider-Verse)",
					Artists: []string{"Post Malone", "Swae Lee"}, LengthSeconds: 158,
				}}, nil)},
			wantStatus: statusBound,
			wantReason: "isrc confirmed by deezer",
		},
		{
			name:  "a remaster of the same recording binds",
			track: celebration(),
			outcomes: []sourceOutcome{outcomeOf(celebration(), sourceDeezer, []candidate{
				with(catalogCelebration("77"), func(c *candidate) { c.Title = "Celebration (2013 Remaster)" }),
			}, nil)},
			wantStatus: statusBound,
			wantReason: "isrc confirmed by deezer",
		},
		{
			name:  "a clean marker binds",
			track: celebration(),
			outcomes: []sourceOutcome{outcomeOf(celebration(), sourceDeezer, []candidate{
				with(catalogCelebration("77"), func(c *candidate) { c.Title = "Celebration (Clean)" }),
			}, nil)},
			wantStatus: statusBound,
			wantReason: "isrc confirmed by deezer",
		},
		{
			name:  "a live version never binds to the studio track",
			track: celebration(),
			outcomes: []sourceOutcome{outcomeOf(celebration(), sourceDeezer, []candidate{
				with(catalogCelebration("77"), func(c *candidate) { c.Title = "Celebration (Live)" }),
			}, nil)},
			wantStatus: statusUnbound,
			wantReason: "refused-title",
		},
		{
			name:  "a featured credit in brackets is not a qualifier",
			track: celebration(),
			outcomes: []sourceOutcome{outcomeOf(celebration(), sourceDeezer, []candidate{
				with(catalogCelebration("77"), func(c *candidate) { c.Title = "Celebration (feat. Someone)" }),
			}, nil)},
			wantStatus: statusBound,
			wantReason: "isrc confirmed by deezer",
		},
		{
			name:  "a candidate carrying another isrc is refused",
			track: celebration(),
			outcomes: []sourceOutcome{outcomeOf(celebration(), sourceDeezer, []candidate{
				with(catalogCelebration("77"), func(c *candidate) { c.ISRC = "USXX00000000" }),
			}, nil)},
			wantStatus: statusUnbound,
			wantReason: "refused-isrc",
		},
		{
			name:  "two sources agreeing record both ids",
			track: celebration(),
			outcomes: []sourceOutcome{
				outcomeOf(celebration(), sourceDeezer, []candidate{catalogCelebration("77")}, nil),
				outcomeOf(celebration(), sourceApple, []candidate{catalogCelebration("a1")}, nil),
			},
			wantStatus: statusBound,
			wantReason: "isrc confirmed by deezer+apple",
		},
		{
			name:  "one failing source does not undo a bind from another",
			track: celebration(),
			outcomes: []sourceOutcome{
				outcomeOf(celebration(), sourceDeezer, nil, errors.New("timeout")),
				outcomeOf(celebration(), sourceMusicBrainz, []candidate{catalogCelebration("mb-1")}, nil),
			},
			wantStatus: statusBound,
			wantReason: "isrc confirmed by musicbrainz",
		},
		{
			name:  "every source failing is a lookup failure",
			track: celebration(),
			outcomes: []sourceOutcome{
				outcomeOf(celebration(), sourceDeezer, nil, errors.New("timeout")),
				outcomeOf(celebration(), sourceApple, nil, errors.New("timeout")),
			},
			wantStatus: statusUnbound,
			wantReason: "lookup-failed",
		},
		{
			name:       "an isrc nobody knows is unbound",
			track:      celebration(),
			outcomes:   []sourceOutcome{outcomeOf(celebration(), sourceDeezer, nil, nil)},
			wantStatus: statusUnbound,
			wantReason: "isrc-not-found",
		},
		{
			name:       "no isrc cannot bind",
			track:      with2(celebration(), func(t *libraryTrack) { t.ISRC = "" }),
			wantStatus: statusUnbound,
			wantReason: "no-isrc",
		},
		{
			name:       "an UNRELEASED album without a confirmation is unofficial",
			track:      with2(celebration(), func(t *libraryTrack) { t.Album = "unreleased" }),
			wantStatus: statusUnofficial,
			wantReason: "album-unreleased",
		},
		{
			name:       "an UNRELEASED album with no isrc is unofficial",
			track:      with2(celebration(), func(t *libraryTrack) { t.Album, t.ISRC = "UNRELEASED", "" }),
			wantStatus: statusUnofficial,
			wantReason: "album-unreleased",
		},
		{
			name:  "an UNRELEASED album whose lookups all answered with nothing is unofficial",
			track: with2(celebration(), func(t *libraryTrack) { t.Album = "UNRELEASED" }),
			outcomes: []sourceOutcome{
				outcomeOf(celebration(), sourceDeezer, nil, nil),
				outcomeOf(celebration(), sourceApple, nil, nil),
			},
			wantStatus: statusUnofficial,
			wantReason: "album-unreleased",
		},
		{
			name:  "an UNRELEASED album with a failed lookup stays unbound",
			track: with2(celebration(), func(t *libraryTrack) { t.Album = "UNRELEASED" }),
			outcomes: []sourceOutcome{
				outcomeOf(celebration(), sourceDeezer, nil, nil),
				outcomeOf(celebration(), sourceApple, nil, errors.New("timeout")),
			},
			wantStatus: statusUnbound,
			wantReason: "isrc-not-found",
		},
		{
			name:  "an UNRELEASED album whose only candidate was refused stays unbound",
			track: with2(celebration(), func(t *libraryTrack) { t.Album = "UNRELEASED" }),
			outcomes: []sourceOutcome{outcomeOf(celebration(), sourceDeezer, []candidate{
				with(catalogCelebration("77"), func(c *candidate) { c.Artists = []string{"Jimmy Eat World"} }),
			}, nil)},
			wantStatus: statusUnbound,
			wantReason: "refused-artist",
		},
		{
			name:  "an UNRELEASED album with every lookup failed stays unbound",
			track: with2(celebration(), func(t *libraryTrack) { t.Album = "UNRELEASED" }),
			outcomes: []sourceOutcome{
				outcomeOf(celebration(), sourceDeezer, nil, errors.New("timeout")),
			},
			wantStatus: statusUnbound,
			wantReason: "lookup-failed",
		},
		{
			name:  "an ambiguous source refuses the bind even when another source confirms",
			track: celebration(),
			outcomes: []sourceOutcome{
				outcomeOf(celebration(), sourceDeezer, []candidate{catalogCelebration("77")}, nil),
				outcomeOf(celebration(), sourceMusicBrainz, []candidate{
					with(catalogCelebration("r0"), func(c *candidate) { c.LengthSeconds = 297.5 }),
					with(catalogCelebration("r1"), func(c *candidate) { c.LengthSeconds = 298.5 }),
				}, nil),
			},
			wantStatus: statusUnbound,
			wantReason: "ambiguous",
		},
		{
			name:       "an UNRELEASED album that a source confirms is bound",
			track:      with2(celebration(), func(t *libraryTrack) { t.Album = "UNRELEASED" }),
			outcomes:   []sourceOutcome{outcomeOf(celebration(), sourceDeezer, []candidate{catalogCelebration("77")}, nil)},
			wantStatus: statusBound,
			wantReason: "isrc confirmed by deezer",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decide(tc.track, tc.outcomes)
			if got.Status != tc.wantStatus || got.Reason != tc.wantReason {
				t.Fatalf("got %s %q, want %s %q", got.Status, got.Reason, tc.wantStatus, tc.wantReason)
			}
		})
	}
}

func with2(t libraryTrack, change func(*libraryTrack)) libraryTrack {
	change(&t)
	return t
}

func TestDecide_BindRecordsTheWinningIDs(t *testing.T) {
	track := celebration()
	got := decide(track, []sourceOutcome{
		outcomeOf(track, sourceDeezer, []candidate{catalogCelebration("77")}, nil),
		outcomeOf(track, sourceMusicBrainz, []candidate{catalogCelebration("mb-1")}, nil),
		outcomeOf(track, sourceApple, []candidate{catalogCelebration("a1")}, nil),
	})
	if got.DeezerTrackID != "77" || got.MBRecordingID != "mb-1" || got.AppleSongID != "a1" || got.ISRC != celebrationISRC {
		t.Fatalf("ids = %+v", got)
	}
	if len(got.Evidence.Sources) != 3 || got.Evidence.FileSeconds != 298 || got.Evidence.Sources[0].Candidates[0].LengthSeconds != 298 {
		t.Fatalf("evidence = %+v", got.Evidence)
	}
}

func TestOutcomeOf_RefusesWhenTheBestTwoAreClose(t *testing.T) {
	track := celebration()
	cases := []struct {
		name        string
		lengths     []float64
		wantWinner  string
		wantAmbigus bool
	}{
		{"two recordings half a second apart are ambiguous", []float64{297.5, 298.5}, "", true},
		{"a clearly closer recording wins", []float64{298, 295.5}, "r0", false},
		{"a lone recording wins", []float64{296}, "r0", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var found []candidate
			for i, length := range tc.lengths {
				id := "r" + string(rune('0'+i))
				found = append(found, with(catalogCelebration(id), func(c *candidate) { c.LengthSeconds = length }))
			}
			got := outcomeOf(track, sourceMusicBrainz, found, nil)
			if got.Winner != tc.wantWinner || got.Ambiguous != tc.wantAmbigus {
				t.Fatalf("winner %q ambiguous %v", got.Winner, got.Ambiguous)
			}
		})
	}
}

func TestOutcomeOf_DuplicateListingsOfOneRecordingAreNotAmbiguous(t *testing.T) {
	track := celebration()
	got := outcomeOf(track, sourceApple, []candidate{
		with(catalogCelebration("b2"), func(c *candidate) { c.Album = "Greatest Hits" }),
		with(catalogCelebration("a1"), func(c *candidate) { c.Album = "Celebrate!" }),
	}, nil)
	if got.Winner != "a1" || got.Ambiguous {
		t.Fatalf("winner %q ambiguous %v", got.Winner, got.Ambiguous)
	}
}
