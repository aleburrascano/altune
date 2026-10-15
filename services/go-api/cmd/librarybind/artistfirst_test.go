package main

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared/songfacts"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const (
	rushLiveISRC   = "CAR401400001"
	rushStudioISRC = "CAR198100001"
)

func rushTomSawyer() libraryTrack {
	return libraryTrack{
		ID: "track-rush", UserID: "user-1", Title: "Tom Sawyer", Artist: "Rush", Album: "Moving Pictures",
		ISRC: rushLiveISRC, FileSeconds: 274, Version: 3,
	}
}

func rushHeld() heldIndex {
	return newHeldIndex([]heldTrack{
		{ID: "track-rush", UserID: "user-1", Artist: "Rush", Album: "Moving Pictures", Title: "Tom Sawyer"},
		{ID: "track-2", UserID: "user-1", Artist: "Rush", Album: "Moving Pictures", Title: "Limelight"},
		{ID: "track-3", UserID: "user-1", Artist: "Rush", Album: "Permanent Waves", Title: "The Spirit of Radio"},
		{ID: "track-4", UserID: "user-2", Artist: "Rush", Album: "Snakes & Arrows", Title: "Far Cry"},
	})
}

func rushCatalogView() catalogView {
	return catalogView{Albums: []string{"Moving Pictures (Remastered)", "Permanent Waves"}, Titles: []string{"Limelight", "Subdivisions"}}
}

func rushSong(id string, change func(*candidate)) candidate {
	c := candidate{ID: id, ISRC: rushStudioISRC, Title: "Tom Sawyer", Artists: []string{"Rush"}, Album: "Moving Pictures", LengthSeconds: 274}
	if change != nil {
		change(&c)
	}
	return c
}

type fakeArtistSource struct {
	name      source
	artists   []providerArtist
	views     map[string]catalogView
	songs     []candidate
	songErr   error
	songCalls int
}

func (f *fakeArtistSource) Name() source { return f.name }

func (f *fakeArtistSource) FindArtists(context.Context, string) ([]providerArtist, error) {
	return f.artists, nil
}

func (f *fakeArtistSource) Catalog(_ context.Context, artist providerArtist) (catalogView, error) {
	return f.views[artist.ID], nil
}

func (f *fakeArtistSource) Songs(context.Context, providerArtist, catalogView, libraryTrack, domain.SearchKeys) ([]candidate, error) {
	f.songCalls++
	return f.songs, f.songErr
}

func rushSource(name source, songs ...candidate) *fakeArtistSource {
	return &fakeArtistSource{
		name:    name,
		artists: []providerArtist{{ID: "rush-1", Name: "Rush"}},
		views:   map[string]catalogView{"rush-1": rushCatalogView()},
		songs:   songs,
	}
}

func musicBrainzKnowing(found ...candidate) isrcLookup {
	calls := 0
	return scriptedLookup{name: sourceMusicBrainz, found: found, calls: &calls}
}

func liveTomSawyerLookups() []isrcLookup {
	calls := 0
	live := candidate{ID: "live-1", ISRC: rushLiveISRC, Title: "Tom Sawyer (Live)", Artists: []string{"Rush"}, LengthSeconds: 301}
	return []isrcLookup{scriptedLookup{name: sourceDeezer, found: []candidate{live}, calls: &calls}}
}

func bindWithArtistFirst(t *testing.T, track libraryTrack, held heldIndex, mb isrcLookup, sources ...artistSource) (decided, error) {
	t.Helper()
	first := newArtistFirst(sources, mb, held)
	done, err := bindTracks(t.Context(), []libraryTrack{track}, liveTomSawyerLookups(), nil, uuid.New(), first.refine)
	if len(done.Decisions) == 0 {
		return decided{}, err
	}
	return done.Decisions[0], err
}

func TestArtistFirst_BindsTheStudioRecordingWhenTheStoredISRCIsAnotherVersion(t *testing.T) {
	studio := rushSong("d1", nil)
	appleStudio := rushSong("a1", nil)
	cases := []struct {
		name         string
		track        libraryTrack
		sources      []artistSource
		mb           isrcLookup
		wantStatus   status
		wantReason   string
		wantISRC     string
		wantReplaced string
	}{
		{
			name:         "deezer and apple agree on the studio isrc",
			track:        rushTomSawyer(),
			sources:      []artistSource{rushSource(sourceDeezer, studio), rushSource(sourceApple, appleStudio)},
			mb:           musicBrainzKnowing(),
			wantStatus:   statusBound,
			wantReason:   "artist-first: isrc confirmed by deezer+apple",
			wantISRC:     rushStudioISRC,
			wantReplaced: rushLiveISRC,
		},
		{
			name:    "one source plus musicbrainz by isrc",
			track:   rushTomSawyer(),
			sources: []artistSource{rushSource(sourceDeezer, studio), rushSource(sourceApple)},
			mb: musicBrainzKnowing(rushSong("mb-1", func(c *candidate) {
				c.Artists = []string{"Rush", "Rush"}
			})),
			wantStatus:   statusBound,
			wantReason:   "artist-first: isrc confirmed by deezer+musicbrainz",
			wantISRC:     rushStudioISRC,
			wantReplaced: rushLiveISRC,
		},
		{
			name:       "one source alone is not enough",
			track:      rushTomSawyer(),
			sources:    []artistSource{rushSource(sourceDeezer, studio), rushSource(sourceApple)},
			mb:         musicBrainzKnowing(),
			wantStatus: statusUnbound,
			wantReason: "refused-title",
		},
		{
			name:  "deezer and apple name different isrcs",
			track: rushTomSawyer(),
			sources: []artistSource{
				rushSource(sourceDeezer, studio),
				rushSource(sourceApple, rushSong("a1", func(c *candidate) { c.ISRC = "CAR198100999" })),
			},
			mb:         musicBrainzKnowing(),
			wantStatus: statusUnbound,
			wantReason: "refused-title",
		},
		{
			name:  "a remix of the song is still refused",
			track: rushTomSawyer(),
			sources: []artistSource{
				rushSource(sourceDeezer, rushSong("d2", func(c *candidate) { c.Title = "Tom Sawyer (Remix)" })),
				rushSource(sourceApple, rushSong("a2", func(c *candidate) { c.Title = "Tom Sawyer (Remix)" })),
			},
			mb:         musicBrainzKnowing(),
			wantStatus: statusUnbound,
			wantReason: "refused-title",
		},
		{
			name:  "a recording more than 3s off the file is refused",
			track: rushTomSawyer(),
			sources: []artistSource{
				rushSource(sourceDeezer, rushSong("d3", func(c *candidate) { c.LengthSeconds = 281 })),
				rushSource(sourceApple, rushSong("a3", func(c *candidate) { c.LengthSeconds = 281 })),
			},
			mb:         musicBrainzKnowing(),
			wantStatus: statusUnbound,
			wantReason: "refused-title",
		},
		{
			name:  "the artist guard stays hard",
			track: rushTomSawyer(),
			sources: []artistSource{
				rushSource(sourceDeezer, rushSong("d4", func(c *candidate) { c.Artists = []string{"Tribute Band"} })),
				rushSource(sourceApple, rushSong("a4", func(c *candidate) { c.Artists = []string{"Tribute Band"} })),
			},
			mb:         musicBrainzKnowing(),
			wantStatus: statusUnbound,
			wantReason: "refused-title",
		},
		{
			name:  "an artist with a common name and no catalog overlap is not bound",
			track: rushTomSawyer(),
			sources: []artistSource{
				&fakeArtistSource{
					name:    sourceDeezer,
					artists: []providerArtist{{ID: "rush-7", Name: "Rush"}, {ID: "rush-8", Name: "Rush"}},
					views:   map[string]catalogView{"rush-7": {Albums: []string{"Unrelated"}}, "rush-8": {Albums: []string{"Other"}}},
					songs:   []candidate{studio},
				},
				&fakeArtistSource{
					name:    sourceApple,
					artists: []providerArtist{{ID: "rush-7", Name: "Rush"}, {ID: "rush-8", Name: "Rush"}},
					views:   map[string]catalogView{"rush-7": {Albums: []string{"Unrelated"}}, "rush-8": {Albums: []string{"Other"}}},
					songs:   []candidate{appleStudio},
				},
			},
			mb:         musicBrainzKnowing(),
			wantStatus: statusUnbound,
			wantReason: "refused-title",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := bindWithArtistFirst(t, tc.track, rushHeld(), tc.mb, tc.sources...)
			if err != nil {
				t.Fatal(err)
			}
			if got.Found.Status != tc.wantStatus || got.Found.Reason != tc.wantReason || got.Found.ISRC != orStored(tc.wantISRC, tc.track.ISRC) {
				t.Fatalf("got %s %q isrc %q", got.Found.Status, got.Found.Reason, got.Found.ISRC)
			}
			replaced := got.Found.Evidence.Replaced
			if tc.wantReplaced == "" && replaced != nil {
				t.Errorf("unexpected replacement %+v", replaced)
			}
			if tc.wantReplaced != "" && (replaced == nil || replaced.ISRC != tc.wantReplaced || replaced.Refusal != "refused-title" || replaced.CatalogSeconds != 274) {
				t.Errorf("replaced = %+v, want old isrc %s", replaced, tc.wantReplaced)
			}
			if len(got.Found.Evidence.ArtistFirst) == 0 {
				t.Error("the artist-first outcomes are not in the evidence")
			}
		})
	}
}

func orStored(want, stored string) string {
	if want != "" {
		return want
	}
	return songfacts.CanonicalISRC(stored)
}

func TestArtistFirst_NoCatalogOverlapNeverSearchesSongs(t *testing.T) {
	ambiguous := &fakeArtistSource{
		name:    sourceDeezer,
		artists: []providerArtist{{ID: "rush-7", Name: "Rush"}, {ID: "rush-8", Name: "Rush"}},
		songs:   []candidate{rushSong("d1", nil)},
	}
	if _, err := bindWithArtistFirst(t, rushTomSawyer(), rushHeld(), musicBrainzKnowing(), ambiguous); err != nil {
		t.Fatal(err)
	}
	if ambiguous.songCalls != 0 {
		t.Errorf("songs searched %d times in an artist nobody could pick", ambiguous.songCalls)
	}
}

func TestArtistFirst_BindsATrackWithNoISRCWithoutAReplacement(t *testing.T) {
	noISRC := rushTomSawyer()
	noISRC.ISRC = ""
	got, err := bindWithArtistFirst(t, noISRC, rushHeld(), musicBrainzKnowing(),
		rushSource(sourceDeezer, rushSong("d1", nil)), rushSource(sourceApple, rushSong("a1", nil)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Found.Status != statusBound || got.Found.ISRC != rushStudioISRC || got.Found.Evidence.Replaced != nil {
		t.Fatalf("got %+v", got.Found)
	}
	if got.Found.DeezerTrackID != "d1" || got.Found.AppleSongID != "a1" {
		t.Errorf("ids = %q %q", got.Found.DeezerTrackID, got.Found.AppleSongID)
	}
}

func TestArtistFirst_LeavesTracksTheISRCPathBoundAlone(t *testing.T) {
	boundTrack := celebration()
	bound := decide(boundTrack, []sourceOutcome{outcomeOf(boundTrack, sourceDeezer, []candidate{catalogCelebration("77")}, nil)})
	deezer := rushSource(sourceDeezer, rushSong("d1", nil))
	got, err := newArtistFirst([]artistSource{deezer}, musicBrainzKnowing(), rushHeld()).refine(t.Context(), boundTrack, bound)
	if err != nil || got.Reason != bound.Reason || deezer.songCalls != 0 {
		t.Fatalf("got %+v, %v, searched %d", got, err, deezer.songCalls)
	}
}

func TestArtistFirst_AbortsWhenACatalogBlocks(t *testing.T) {
	blockedDeezer := rushSource(sourceDeezer)
	blockedDeezer.songErr = blocked("HTTP 403")
	_, err := bindWithArtistFirst(t, rushTomSawyer(), rushHeld(), musicBrainzKnowing(), blockedDeezer, rushSource(sourceApple))
	if !errors.Is(err, errCatalogBlocked) || !strings.Contains(err.Error(), "aborted after 0 of 1 tracks") {
		t.Fatalf("err = %v", err)
	}
}

func TestArtistFirst_ChoosesNothingWhenAnExactNameSitsPastTheFetchedHits(t *testing.T) {
	crowded := &fakeArtistSource{
		name: sourceDeezer,
		artists: []providerArtist{
			{ID: "1", Name: "Rush"}, {ID: "2", Name: "Rush Hour"}, {ID: "3", Name: "Rush Rush"}, {ID: "4", Name: "RUSH"},
		},
		views: map[string]catalogView{},
		songs: []candidate{rushSong("d1", nil)},
	}
	if _, err := bindWithArtistFirst(t, rushTomSawyer(), rushHeld(), musicBrainzKnowing(), crowded); err != nil {
		t.Fatal(err)
	}
	if crowded.songCalls != 0 {
		t.Errorf("songs searched %d times although two artists share the exact name", crowded.songCalls)
	}
}

func TestArtistFirst_ASourceWithTooManyMatchesRefusesTheBind(t *testing.T) {
	crowdedDeezer := rushSource(sourceDeezer)
	crowdedDeezer.songErr = errTooManyMatches
	got, err := bindWithArtistFirst(t, rushTomSawyer(), rushHeld(), musicBrainzKnowing(rushSong("mb-1", nil)),
		crowdedDeezer, rushSource(sourceApple, rushSong("a1", nil)))
	if err != nil || got.Found.Status != statusUnbound {
		t.Fatalf("got %+v, %v", got.Found, err)
	}
}

func TestArtistFirst_ASourceNamingTwoISRCsForTheFileRefusesTheBind(t *testing.T) {
	twoISRCs := rushSource(sourceDeezer, rushSong("d1", nil), rushSong("d2", func(c *candidate) { c.ISRC = "CAR198100777" }))
	got, err := bindWithArtistFirst(t, rushTomSawyer(), rushHeld(), musicBrainzKnowing(rushSong("mb-1", nil)),
		twoISRCs, rushSource(sourceApple, rushSong("a1", nil)))
	if err != nil || got.Found.Status != statusUnbound {
		t.Fatalf("got %+v, %v", got.Found, err)
	}
}

func TestChooseArtist_CountsEachHeldTrackOnce(t *testing.T) {
	owned := holdings{{Album: "live", Title: "intro"}}
	namesake := artistOption{providerArtist: providerArtist{ID: "1", Name: "Rush Band"}, view: catalogView{Albums: []string{"Live"}, Titles: []string{"Intro"}}}
	if got, ok := chooseArtist(owned, "Rush", []artistOption{namesake}); ok {
		t.Fatalf("one held track reached the overlap bar by hitting twice: %+v", got)
	}
}

func TestHeldIndex_ExcludesTheTrackItselfAndOtherUsers(t *testing.T) {
	owned := rushHeld().holdingsFor(rushTomSawyer())
	want := holdings{{Album: "moving pictures", Title: "limelight"}, {Album: "permanent waves", Title: "the spirit of radio"}}
	if len(owned) != len(want) || owned[0] != want[0] || owned[1] != want[1] {
		t.Errorf("holdings = %+v, want %+v", owned, want)
	}
}

func TestChooseArtist_NeedsTwoOverlapsOrTheOnlyExactName(t *testing.T) {
	owned := rushHeld().holdingsFor(rushTomSawyer())
	overlapping := artistOption{providerArtist: providerArtist{ID: "1", Name: "Rush (CA)"}, view: rushCatalogView()}
	exactNoOverlap := artistOption{providerArtist: providerArtist{ID: "2", Name: "Rush"}, view: catalogView{Albums: []string{"Other"}}}
	otherExact := artistOption{providerArtist: providerArtist{ID: "3", Name: "RUSH"}, view: catalogView{Albums: []string{"Else"}}}
	cases := []struct {
		name    string
		options []artistOption
		wantID  string
	}{
		{"two overlaps win over an exact name", []artistOption{exactNoOverlap, overlapping}, "1"},
		{"the only exact name is enough", []artistOption{exactNoOverlap}, "2"},
		{"two exact names with no overlap pick nothing", []artistOption{exactNoOverlap, otherExact}, ""},
		{"a tie in overlap picks nothing", []artistOption{overlapping, {providerArtist: providerArtist{ID: "4", Name: "Rush"}, view: rushCatalogView()}}, ""},
		{"nothing found", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := chooseArtist(owned, "Rush", tc.options)
			if ok != (tc.wantID != "") || got.ID != tc.wantID {
				t.Fatalf("got %+v %v, want %q", got, ok, tc.wantID)
			}
		})
	}
}
