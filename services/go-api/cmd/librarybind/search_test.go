package main

import (
	"altune/go-api/internal/shared/releasepick"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"

	"github.com/google/uuid"
)

type fakeSearch struct {
	found   []candidate
	err     error
	queries [][2]string
}

func (f *fakeSearch) Name() source { return sourceMusicBrainz }

func (f *fakeSearch) Search(_ context.Context, artist, title string) ([]candidate, error) {
	f.queries = append(f.queries, [2]string{artist, title})
	return f.found, f.err
}

type fakeDeezerByISRC struct {
	known map[string]candidate
	fail  map[string]error
	asked []string
}

func (f *fakeDeezerByISRC) Name() source { return sourceDeezer }

func (f *fakeDeezerByISRC) ByISRC(_ context.Context, isrc string) ([]candidate, error) {
	f.asked = append(f.asked, isrc)
	if err := f.fail[isrc]; err != nil {
		return nil, err
	}
	if c, ok := f.known[isrc]; ok {
		return []candidate{c}, nil
	}
	return nil, nil
}

var studioAlbum = []releasepick.Group{{PrimaryType: "Album", Date: "1990"}}

func bohemianRhapsody() libraryTrack {
	return libraryTrack{ID: "t-queen", UserID: "u1", Title: "Bohemian Rhapsody", Artist: "Queen", Album: "A Night at the Opera", FileSeconds: 354.4, Version: 2}
}

func queenRecording(id, isrc, released string, seconds float64) candidate {
	return candidate{ID: id, ISRC: isrc, Title: "Bohemian Rhapsody", Artists: []string{"Queen"}, LengthSeconds: seconds, Homes: []releasepick.Group{{PrimaryType: "Album", Date: released}}}
}

func onDeezer(c candidate) candidate {
	c.ID, c.Homes = "dz-"+c.ISRC, nil
	return c
}

func searchWith(musicBrainz []candidate, onDeezerToo ...candidate) (*fakeSearch, *fakeDeezerByISRC, searchBinder) {
	mb := &fakeSearch{found: musicBrainz}
	dz := &fakeDeezerByISRC{known: map[string]candidate{}, fail: map[string]error{}}
	for _, c := range onDeezerToo {
		dz.known[c.ISRC] = onDeezer(c)
	}
	return mb, dz, searchBinder{musicBrainz: mb, deezer: dz}
}

func noISRC() identity { return identity{Status: statusUnbound, Reason: reasonNoISRC} }

func TestSearchBinder_BindsATrackWithoutISRCToTheFirstISRCDeezerConfirms(t *testing.T) {
	single := queenRecording("mb-single", "GBCEE7500015", "1977", 355.1)
	sameRecording := queenRecording("mb-single", "GBUM71029604", "1977", 355.1)
	album := queenRecording("mb-album", "GBCEE0500364", "2005", 354.0)
	live := queenRecording("mb-live", "GBCEE0300050", "", 350.6)
	mb, dz, binder := searchWith([]candidate{live, album, single, sameRecording}, sameRecording)
	got, err := binder.refine(t.Context(), bohemianRhapsody(), noISRC())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != statusBound || got.ISRC != "GBUM71029604" || got.Reason != "search: isrc confirmed by musicbrainz+deezer" {
		t.Fatalf("got %s %q isrc %q", got.Status, got.Reason, got.ISRC)
	}
	if got.MBRecordingID != "mb-single" || got.DeezerTrackID != "dz-GBUM71029604" {
		t.Errorf("ids = mb %q deezer %q", got.MBRecordingID, got.DeezerTrackID)
	}
	if mb.queries[0] != [2]string{"Queen", "Bohemian Rhapsody"} {
		t.Errorf("query = %v", mb.queries)
	}
	if !slices.Equal(dz.asked, []string{"GBCEE7500015", "GBUM71029604"}) {
		t.Errorf("deezer asked %v, want the earliest release first and no live isrc", dz.asked)
	}
	if len(got.Evidence.Search) != 3 {
		t.Errorf("search evidence = %+v", got.Evidence.Search)
	}
}

func TestSearchBinder_BindsThroughTheBindLoop(t *testing.T) {
	studio := queenRecording("mb-album", "GBCEE0500364", "2005", 354.0)
	_, _, binder := searchWith([]candidate{studio}, studio)
	done, err := bindTracks(t.Context(), []libraryTrack{bohemianRhapsody()}, nil, nil, uuid.New(), binder.refine)
	if err != nil || len(done.Decisions) != 1 {
		t.Fatalf("done %+v, %v", done, err)
	}
	if got := done.Decisions[0].Found; got.Status != statusBound || got.ISRC != "GBCEE0500364" {
		t.Fatalf("got %s %q isrc %q", got.Status, got.Reason, got.ISRC)
	}
}

func TestSearchBinder_LeavesAlreadyDecidedTracksAlone(t *testing.T) {
	studio := queenRecording("mb-album", "GBCEE0500364", "2005", 354.0)
	mb, _, binder := searchWith([]candidate{studio}, studio)
	for _, decided := range []identity{
		{Status: statusBound, ISRC: "GBCEE0500364"},
		{Status: statusUnofficial, Reason: reasonAlbumUnreleased},
		{Status: statusUnbound, Reason: reasonLookupFailed},
	} {
		got, err := binder.refine(t.Context(), bohemianRhapsody(), decided)
		if err != nil || got.Status != decided.Status || got.Reason != decided.Reason {
			t.Fatalf("%+v became %+v, %v", decided, got, err)
		}
	}
	if len(mb.queries) != 0 {
		t.Errorf("searched %d times", len(mb.queries))
	}
}

func TestSearchBinder_CleansTheArtistOutOfTheTitleForMatching(t *testing.T) {
	myWay := libraryTrack{ID: "t-elvis", UserID: "u1", Title: "My Way - Elvis Presley", Artist: "Elvis Presley", FileSeconds: 227}
	elvis := candidate{ID: "mb-1", ISRC: "USRC17705642", Title: "My Way", Artists: []string{"Elvis Presley"}, LengthSeconds: 228.7, Homes: studioAlbum}
	mb, _, binder := searchWith([]candidate{elvis}, elvis)
	got, err := binder.refine(t.Context(), myWay, noISRC())
	if err != nil || got.Status != statusBound || got.ISRC != "USRC17705642" {
		t.Fatalf("got %s %q isrc %q, %v", got.Status, got.Reason, got.ISRC, err)
	}
	if mb.queries[0] != [2]string{"Elvis Presley", "My Way"} {
		t.Errorf("query = %v", mb.queries[0])
	}
}

func TestSearchBinder_SearchesTheLeadArtistOfAFeaturedCredit(t *testing.T) {
	fallin := libraryTrack{ID: "t-usher", UserID: "u1", Title: "DJ Got Us Fallin' in Love", Artist: "Usher featuring Pitbull", FileSeconds: 221.5}
	song := candidate{ID: "x", ISRC: "USLF21000041", Title: "DJ Got Us Fallin’ In Love", Artists: []string{"Usher"}, LengthSeconds: 221.7, Homes: studioAlbum}
	mb, _, binder := searchWith([]candidate{song}, song)
	got, err := binder.refine(t.Context(), fallin, noISRC())
	if err != nil || got.Status != statusBound {
		t.Fatalf("got %s %q, %v", got.Status, got.Reason, err)
	}
	if mb.queries[0][0] != "Usher" {
		t.Errorf("artist queried = %q", mb.queries[0][0])
	}
}

func TestSearchBinder_LeavesTracksUnboundWhenTheSourcesDoNotProveOneRecording(t *testing.T) {
	getYou := libraryTrack{ID: "t-caesar", UserID: "u1", Title: "Get You (feat. Kali Uchis)", Artist: "Daniel Caesar", FileSeconds: 271}
	getYouListing := func(seconds float64) candidate {
		return candidate{ID: "g", ISRC: "TCACR1690004", Title: "Get You (feat. Kali Uchis)", Artists: []string{"Daniel Caesar"}, LengthSeconds: seconds, Homes: studioAlbum}
	}
	studio := queenRecording("mb-album", "GBCEE0500364", "2005", 354.0)
	liveTitle := func(c candidate) candidate {
		return with(c, func(c *candidate) { c.Title = "Bohemian Rhapsody (Live)" })
	}
	cases := []struct {
		name        string
		track       libraryTrack
		musicBrainz []candidate
		deezer      []candidate
	}{
		{"the catalog length is seven seconds off the file", getYou, []candidate{getYouListing(278)}, []candidate{getYouListing(278)}},
		{"deezer does not know the proposed isrc", bohemianRhapsody(), []candidate{studio}, nil},
		{"deezer knows it at another length", bohemianRhapsody(), []candidate{studio}, []candidate{with(studio, func(c *candidate) { c.LengthSeconds = 330 })}},
		{"the only matching recording is a live one", bohemianRhapsody(), []candidate{liveTitle(studio)}, []candidate{liveTitle(studio)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, binder := searchWith(tc.musicBrainz, tc.deezer...)
			got, err := binder.refine(t.Context(), tc.track, identity{Status: statusUnbound, Reason: "refused-length"})
			if err != nil || got.Status != statusUnbound || got.Reason != "refused-length" || got.ISRC != "" {
				t.Fatalf("got %s %q isrc %q, %v", got.Status, got.Reason, got.ISRC, err)
			}
		})
	}
}

func TestSearchBinder_PrefersTheOriginalReleaseThenTheClosestLength(t *testing.T) {
	cases := []struct {
		name        string
		musicBrainz []candidate
		wantFirst   string
	}{
		{
			"the earlier plain release wins over a reissue",
			[]candidate{queenRecording("a", "GBUM71029604", "2011", 354.4), queenRecording("b", "GBCEE7500015", "1975-11-21", 355.0)},
			"GBCEE7500015",
		},
		{
			"a release date beats having none",
			[]candidate{queenRecording("a", "GBCEE7500015", "", 354.4), queenRecording("b", "GBUM71029604", "2011", 355.0)},
			"GBUM71029604",
		},
		{
			"the same year falls to the closer length",
			[]candidate{queenRecording("a", "GBCEE7500015", "1975", 356.0), queenRecording("b", "GBUM71029604", "1975", 354.5)},
			"GBUM71029604",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, dz, binder := searchWith(tc.musicBrainz, tc.musicBrainz...)
			got, err := binder.refine(t.Context(), bohemianRhapsody(), noISRC())
			if err != nil || got.ISRC != tc.wantFirst || dz.asked[0] != tc.wantFirst {
				t.Fatalf("isrc %q asked %v (%s %q), %v", got.ISRC, dz.asked, got.Status, got.Reason, err)
			}
		})
	}
}

func TestSearchBinder_AsksDeezerAboutAtMostFiveISRCs(t *testing.T) {
	var proposals []candidate
	for i := range 8 {
		proposals = append(proposals, queenRecording("mb-"+strconv.Itoa(i), "GBCEE000000"+strconv.Itoa(i), "", 354.4))
	}
	_, dz, binder := searchWith(proposals)
	got, err := binder.refine(t.Context(), bohemianRhapsody(), noISRC())
	if err != nil || got.Status != statusUnbound || len(dz.asked) != maxISRCsConfirmed {
		t.Fatalf("%s, %v, deezer asked %v", got.Status, err, dz.asked)
	}
}

func TestSearchBinder_RecordsTheStoredISRCItReplaces(t *testing.T) {
	nunToMe := libraryTrack{ID: "t-kankan", UserID: "u1", Title: "Nun To Me 2", Artist: "KANKAN", ISRC: "USUYG1412120", FileSeconds: 126.7}
	sequel := candidate{ID: "s", ISRC: "USUYG1414898", Title: "Nun To Me 2", Artists: []string{"KANKAN"}, LengthSeconds: 126, Homes: studioAlbum}
	_, _, binder := searchWith([]candidate{sequel}, sequel)
	got, err := binder.refine(t.Context(), nunToMe, identity{Status: statusUnbound, Reason: "refused-title"})
	if err != nil || got.Status != statusBound || got.ISRC != "USUYG1414898" {
		t.Fatalf("got %s %q isrc %q, %v", got.Status, got.Reason, got.ISRC, err)
	}
	if got.Evidence.Replaced == nil || got.Evidence.Replaced.ISRC != "USUYG1412120" || got.Evidence.Replaced.Refusal != "refused-title" || got.Evidence.Replaced.Independent {
		t.Errorf("replaced = %+v", got.Evidence.Replaced)
	}
}

func TestSearchBinder_SourceFailures(t *testing.T) {
	studio := queenRecording("mb-album", "GBCEE0500364", "2005", 354.0)
	t.Run("a blocked musicbrainz stops the run", func(t *testing.T) {
		mb, _, binder := searchWith(nil)
		mb.err = blocked("HTTP 403")
		if _, err := binder.refine(t.Context(), bohemianRhapsody(), noISRC()); !errors.Is(err, errCatalogBlocked) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a blocked deezer stops the run", func(t *testing.T) {
		_, dz, binder := searchWith([]candidate{studio})
		dz.fail["GBCEE0500364"] = blocked("HTTP 403")
		if _, err := binder.refine(t.Context(), bohemianRhapsody(), noISRC()); !errors.Is(err, errCatalogBlocked) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a failing musicbrainz search leaves the track unbound and says why", func(t *testing.T) {
		mb, _, binder := searchWith(nil)
		mb.err = errors.New("fetch: status 500")
		got, err := binder.refine(t.Context(), bohemianRhapsody(), noISRC())
		if err != nil || got.Status != statusUnbound || got.Evidence.Search[0].Error == "" {
			t.Fatalf("got %s, %v, evidence %+v", got.Status, err, got.Evidence.Search)
		}
	})
	t.Run("a deezer error on one isrc does not stop the next", func(t *testing.T) {
		other := queenRecording("mb-single", "GBCEE7500015", "2010", 354.4)
		_, dz, binder := searchWith([]candidate{studio, other}, other)
		dz.fail["GBCEE0500364"] = errors.New("deezer error code 300")
		got, err := binder.refine(t.Context(), bohemianRhapsody(), noISRC())
		if err != nil || got.ISRC != "GBCEE7500015" {
			t.Fatalf("got %s %q isrc %q, %v", got.Status, got.Reason, got.ISRC, err)
		}
	})
}

const queenSearchJSON = `{"recordings":[
	{"id":"mb-studio","title":"Bohemian Rhapsody","length":354000,"isrcs":["GBCEE0500364","GBUM71029604"],
	 "artist-credit":[{"name":"Queen","artist":{"name":"Queen"}}],
	 "releases":[{"date":"2011","release-group":{"primary-type":"Album"}},{"date":"1975-10-31","release-group":{"primary-type":"Album"}},
	             {"date":"1990","release-group":{"primary-type":"Album","secondary-types":["Compilation"]}}]},
	{"id":"mb-live","title":"Bohemian Rhapsody","length":350573,"isrcs":["GBCEE0300050"],
	 "artist-credit":[{"name":"Queen","artist":{"name":"Queen"}}],
	 "releases":[{"date":"1992-06-24","release-group":{"primary-type":"Album","secondary-types":["Live"]}}]}]}`

func TestMusicBrainzSearch_QueriesByTitleAndArtistAndYieldsOneCandidatePerISRC(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("query"); got != `recording:"Bohemian \"Rhapsody\"" AND artist:"Queen" AND isrc:*` {
			t.Errorf("query = %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != strconv.Itoa(musicBrainzSearchLimit) {
			t.Errorf("limit = %q", got)
		}
		_, _ = w.Write([]byte(queenSearchJSON))
	}))
	defer srv.Close()
	got, err := newMusicBrainzLookup(srv.Client(), srv.URL, 1000).Search(t.Context(), "Queen", `Bohemian "Rhapsody"`)
	if err != nil || len(got) != 3 {
		t.Fatalf("got %+v, %v", got, err)
	}
	studio := got[0]
	if studio.ID != "mb-studio" || studio.ISRC != "GBCEE0500364" || studio.LengthSeconds != 354 || len(studio.Homes) != 3 || !artistMatches("Queen", studio.Artists) {
		t.Errorf("studio candidate = %+v", studio)
	}
	if home, kind, ok := studio.original(); !ok || home.Date != "1975-10-31" || kind != releasepick.KindStudioAlbum {
		t.Errorf("studio original = %+v %v %v", home, kind, ok)
	}
	if got[1].ISRC != "GBUM71029604" || got[2].ID != "mb-live" || got[2].Disambiguation != "" {
		t.Errorf("candidates = %+v", got)
	}
	if _, _, ok := got[2].original(); ok {
		t.Errorf("a recording that only appears on a live album has an original release")
	}
}

const princeSearchJSON = `{"recordings":[
	{"id":"392bd99f-4f36-4e5c-adbd-7287ab92c381","title":"1999","length":217413,"disambiguation":"single edit",
	 "isrcs":["USRH11902862","USWB19901185","USWB18200029"],
	 "artist-credit":[{"name":"Prince","artist":{"name":"Prince","aliases":[{"name":"(us 1) PRINCE"}]}}],
	 "releases":[{"date":"2019-11-29","release-group":{"primary-type":"Album"}},{"date":"1998","release-group":{"primary-type":"Album","secondary-types":["Compilation"]}}]},
	{"id":"d2c83920-6066-48f4-88d6-2ad6f469ecf4","title":"1999","length":378000,"disambiguation":"album version",
	 "isrcs":["USRH11902851","USWB18200028"],
	 "artist-credit":[{"name":"Prince","artist":{"name":"Prince"}}],
	 "releases":[{"date":"2019-11-29","release-group":{"primary-type":"Album"}},{"date":"1982","release-group":{"primary-type":"Album"}}]},
	{"id":"4b2d1f0e-8c0a-4f55-9d3e-0a6a3b6f6f11","title":"1999","length":353943,"disambiguation":"live, 1987-06-20: Stadion Galgenwaard, Utrecht, Netherlands",
	 "isrcs":["USWB12000944"],
	 "artist-credit":[{"name":"Prince","artist":{"name":"Prince"}}],
	 "releases":[{"date":"2020","release-group":{"primary-type":"Album","secondary-types":["Live"]}}]}]}`

func TestSearchBinder_BindsTheSingleEditMusicBrainzRecordedForAFileOfThatLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(princeSearchJSON)) }))
	defer srv.Close()
	nineteenNinetyNine := libraryTrack{ID: "t-prince", UserID: "u1", Title: "1999", Artist: "Prince", Album: "1999", FileSeconds: 218.88, Version: 1}
	singleEdit := candidate{ID: "dz-1", ISRC: "USWB19901185", Title: "1999", Artists: []string{"Prince"}, LengthSeconds: 217}
	dz := &fakeDeezerByISRC{known: map[string]candidate{singleEdit.ISRC: singleEdit}, fail: map[string]error{}}
	binder := searchBinder{musicBrainz: newMusicBrainzLookup(srv.Client(), srv.URL, 1000), deezer: dz}
	got, err := binder.refine(t.Context(), nineteenNinetyNine, noISRC())
	if err != nil || got.Status != statusBound || got.ISRC != "USWB19901185" || got.MBRecordingID != "392bd99f-4f36-4e5c-adbd-7287ab92c381" {
		t.Fatalf("got %s %q isrc %q mb %q, %v", got.Status, got.Reason, got.ISRC, got.MBRecordingID, err)
	}
	if !slices.Equal(dz.asked, []string{"USRH11902862", "USWB18200029", "USWB19901185"}) {
		t.Errorf("deezer asked %v, want only the single edit's isrcs", dz.asked)
	}
}

func TestMusicBrainzSearch_AbortsOnForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer srv.Close()
	if _, err := newMusicBrainzLookup(srv.Client(), srv.URL, 1000).Search(t.Context(), "Queen", "Bohemian Rhapsody"); !errors.Is(err, errCatalogBlocked) {
		t.Fatalf("err = %v, want blocked", err)
	}
}

func TestSearchBinder_BindsOnlyRecordingsWithAnOriginalStudioRelease(t *testing.T) {
	onAlbum := func(secondary ...string) []releasepick.Group {
		return []releasepick.Group{{PrimaryType: "Album", Date: "1992", SecondaryTypes: secondary}}
	}
	studio := queenRecording("mb-studio", "GBCEE0500364", "1975", 354.9)
	cases := []struct {
		name      string
		recording candidate
		wantBound bool
	}{
		{"a plain title on a plain album", studio, true},
		{"a plain title that only appears on a live album", with(studio, func(c *candidate) { c.Homes = onAlbum("Live") }), false},
		{"a plain title that only appears on a compilation", with(studio, func(c *candidate) { c.Homes = onAlbum("Compilation") }), false},
		{"a plain title that only appears on a remix single", with(studio, func(c *candidate) {
			c.Homes = []releasepick.Group{{PrimaryType: "Single", SecondaryTypes: []string{"Remix"}}}
		}), false},
		{"a recording MusicBrainz disambiguates", with(studio, func(c *candidate) { c.Disambiguation = "demo" }), false},
		{"a recording with no release data", with(studio, func(c *candidate) { c.Homes = nil }), false},
		{"a live album next to a plain album is still an original", with(studio, func(c *candidate) {
			c.Homes = append(onAlbum("Live"), releasepick.Group{PrimaryType: "Album", Date: "1975"})
		}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, dz, binder := searchWith([]candidate{tc.recording}, tc.recording)
			got, err := binder.refine(t.Context(), bohemianRhapsody(), noISRC())
			if err != nil || (got.Status == statusBound) != tc.wantBound {
				t.Fatalf("got %s %q, %v", got.Status, got.Reason, err)
			}
			if !tc.wantBound && len(dz.asked) != 0 {
				t.Errorf("deezer asked %v about a recording that cannot bind", dz.asked)
			}
		})
	}
}

func TestSearchBinder_BindsRecordingsMusicBrainzOnlyLabelsAsAStudioCut(t *testing.T) {
	studio := queenRecording("mb-studio", "GBCEE0500364", "1975", 354.9)
	cases := []struct {
		disambiguation string
		wantBound      bool
	}{
		{"single edit", true},
		{"radio edit", true},
		{"album version", true},
		{"single version", true},
		{"album original mix", true},
		{"live, 1992-04-20: Wembley Stadium, London, UK", false},
		{"acoustic", false},
		{"part of “Club MTV” DJ‐mix", false},
		{"360 Reality Audio mix", false},
		{"alternate take", false},
	}
	for _, tc := range cases {
		t.Run(tc.disambiguation, func(t *testing.T) {
			recording := with(studio, func(c *candidate) { c.Disambiguation = tc.disambiguation })
			_, _, binder := searchWith([]candidate{recording}, recording)
			got, err := binder.refine(t.Context(), bohemianRhapsody(), noISRC())
			if err != nil || (got.Status == statusBound) != tc.wantBound {
				t.Fatalf("got %s %q, %v", got.Status, got.Reason, err)
			}
		})
	}
}

func TestSearchBinder_BindsTheOriginalWhenOnlyTheFileLengthDiffersFromTheCatalog(t *testing.T) {
	closer := libraryTrack{ID: "t-rush", UserID: "u1", Title: "Closer to the Heart", Artist: "Rush", FileSeconds: 188.67, Version: 1}
	studio := candidate{ID: "mb-1", ISRC: "USMR17780107", Title: "Closer to the Heart", Artists: []string{"Rush"}, LengthSeconds: 174, Homes: studioAlbum}
	_, dz, binder := searchWith([]candidate{studio}, studio)
	done, err := bindTracks(t.Context(), []libraryTrack{closer}, nil, nil, uuid.New(), binder.refine, binder.refineDifferingCut)
	if err != nil || len(done.Decisions) != 1 {
		t.Fatalf("done %+v, %v", done, err)
	}
	got := done.Decisions[0].Found
	if got.Status != statusBound || got.ISRC != "USMR17780107" || got.Reason != "search: bound: file length differs from catalog by 15s" {
		t.Fatalf("got %s %q isrc %q", got.Status, got.Reason, got.ISRC)
	}
	if got.MBRecordingID != "mb-1" || got.DeezerTrackID != "dz-USMR17780107" || got.Evidence.DifferingCut == nil || got.Evidence.DifferingCut.CatalogSeconds != 174 {
		t.Errorf("ids = mb %q deezer %q, evidence %+v", got.MBRecordingID, got.DeezerTrackID, got.Evidence.DifferingCut)
	}
	if len(dz.asked) != 1 {
		t.Errorf("deezer asked %v", dz.asked)
	}
}

func TestSearchBinder_ADifferingCutNeedsDeezerToAgreeWithMusicBrainzAndStaysAnOriginal(t *testing.T) {
	closer := libraryTrack{ID: "t-rush", UserID: "u1", Title: "Closer to the Heart", Artist: "Rush", FileSeconds: 188.67, Version: 1}
	studio := candidate{ID: "mb-1", ISRC: "USMR17780107", Title: "Closer to the Heart", Artists: []string{"Rush"}, LengthSeconds: 174, Homes: studioAlbum}
	cases := []struct {
		name        string
		musicBrainz candidate
		deezer      []candidate
	}{
		{"deezer lists the isrc at another length", studio, []candidate{with(studio, func(c *candidate) { c.LengthSeconds = 190 })}},
		{"deezer does not know the isrc", studio, nil},
		{"the recording only appears on a compilation", with(studio, func(c *candidate) {
			c.Homes = []releasepick.Group{{PrimaryType: "Album", SecondaryTypes: []string{"Compilation"}, Date: "1987"}}
		}), []candidate{studio}},
		{"the title names a live version", with(studio, func(c *candidate) { c.Title = "Closer to the Heart (Live)" }), []candidate{studio}},
		{"the artist is another act", with(studio, func(c *candidate) { c.Artists = []string{"Big Wreck"} }), []candidate{studio}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, binder := searchWith([]candidate{tc.musicBrainz}, tc.deezer...)
			got, err := bindTracks(t.Context(), []libraryTrack{closer}, nil, nil, uuid.New(), binder.refine, binder.refineDifferingCut)
			if err != nil || got.Decisions[0].Found.Status != statusUnbound {
				t.Fatalf("got %+v, %v", got.Decisions[0].Found, err)
			}
		})
	}
}

func TestSearchBinder_ADifferingCutPastTheCapStaysRefusedWithTheGapInTheEvidence(t *testing.T) {
	closer := libraryTrack{ID: "t-rush", UserID: "u1", Title: "Closer to the Heart", Artist: "Rush", FileSeconds: 188.67, Version: 1}
	studio := candidate{ID: "mb-1", ISRC: "USMR17780107", Title: "Closer to the Heart", Artists: []string{"Rush"}, LengthSeconds: 174, Homes: studioAlbum}
	_, dz, binder := searchWith([]candidate{studio}, studio)
	binder.cuts = cutPolicy{maxGapSeconds: 10}
	done, err := bindTracks(t.Context(), []libraryTrack{closer}, nil, nil, uuid.New(), binder.refine, binder.refineDifferingCut)
	if err != nil {
		t.Fatal(err)
	}
	got := done.Decisions[0].Found
	if got.Status != statusUnbound || got.Reason != "refused-length" || got.Evidence.DifferingCut == nil || got.Evidence.DifferingCut.CatalogSeconds != 174 || len(dz.asked) != 0 {
		t.Fatalf("got %s %q cut %+v, deezer asked %v", got.Status, got.Reason, got.Evidence.DifferingCut, dz.asked)
	}
}

func TestSearchBinder_ADifferingCutYieldsToARecordingAtTheFilesOwnLength(t *testing.T) {
	iris := libraryTrack{ID: "t-iris", UserID: "u1", Title: "Iris", Artist: "The Goo Goo Dolls", FileSeconds: 289, Version: 1}
	rerecording := candidate{ID: "mb-1", ISRC: "USWB12600244", Title: "Iris", Artists: []string{"Goo Goo Dolls"}, LengthSeconds: 199.7, Homes: studioAlbum}
	original := candidate{ID: "dz-1", ISRC: "USWB19800160", Title: "Iris", Artists: []string{"The Goo Goo Dolls"}, LengthSeconds: 289}
	_, dz, binder := searchWith([]candidate{rerecording}, rerecording)
	found := noISRC()
	found.Evidence.ArtistFirst = []sourceOutcome{{Source: sourceDeezer, Candidates: []judged{judge(iris, original)}}}
	strict, err := binder.refine(t.Context(), iris, found)
	if err != nil {
		t.Fatal(err)
	}
	got, err := binder.refineDifferingCut(t.Context(), iris, strict)
	if err != nil || got.Status != statusUnbound || got.Evidence.DifferingCut != nil || len(dz.asked) != 0 {
		t.Fatalf("got %s %q cut %+v, deezer asked %v, %v", got.Status, got.Reason, got.Evidence.DifferingCut, dz.asked, err)
	}
}

func TestSearchBinder_ADifferingCutPassOnlyRunsForTracksTheSearchLeftUnbound(t *testing.T) {
	studio := queenRecording("mb-album", "GBCEE0500364", "2005", 354.0)
	_, dz, binder := searchWith([]candidate{studio}, studio)
	for _, decided := range []identity{
		{Status: statusBound, ISRC: "GBCEE0500364"},
		{Status: statusUnbound, Reason: reasonLookupFailed},
		{Status: statusUnbound, Reason: reasonNoISRC},
	} {
		got, err := binder.refineDifferingCut(t.Context(), bohemianRhapsody(), decided)
		if err != nil || got.Status != decided.Status || got.Reason != decided.Reason {
			t.Fatalf("%+v became %+v, %v", decided, got, err)
		}
	}
	if len(dz.asked) != 0 {
		t.Errorf("deezer asked %v", dz.asked)
	}
}

func TestSearchBinder_RanksByTheSharedReleasePickBeforeDateAndLength(t *testing.T) {
	single := with(queenRecording("a", "GBCEE7500015", "", 354.4), func(c *candidate) {
		c.Homes = []releasepick.Group{{PrimaryType: "Single", Date: "1975-10"}}
	})
	album := queenRecording("b", "GBUM71029604", "1976", 355.0)
	_, dz, binder := searchWith([]candidate{single, album}, single, album)
	got, err := binder.refine(t.Context(), bohemianRhapsody(), noISRC())
	if err != nil || got.ISRC != "GBUM71029604" || dz.asked[0] != "GBUM71029604" {
		t.Fatalf("isrc %q asked %v, %v", got.ISRC, dz.asked, err)
	}
}
