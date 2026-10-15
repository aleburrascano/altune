package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func caesarTrack() libraryTrack {
	return libraryTrack{ID: "t-caesar", UserID: "u1", Title: "Get You (feat. Kali Uchis)", Artist: "Daniel Caesar", ISRC: "TCACR1690004", FileSeconds: 271, Version: 1}
}

func caesarListing(id string, length float64) candidate {
	return candidate{ID: id, ISRC: "TCACR1690004", Title: "Get You (feat. Kali Uchis)", Artists: []string{"Daniel Caesar"}, LengthSeconds: length}
}

func lookupsListing(listings map[source][]candidate) []isrcLookup {
	var lookups []isrcLookup
	for _, name := range []source{sourceDeezer, sourceMusicBrainz, sourceApple} {
		calls := 0
		lookups = append(lookups, scriptedLookup{name: name, found: listings[name], calls: &calls})
	}
	return lookups
}

func bindWithDifferingCut(t *testing.T, track libraryTrack, listings map[source][]candidate) identity {
	t.Helper()
	done, err := bindTracks(t.Context(), []libraryTrack{track}, lookupsListing(listings), nil, uuid.New(), cutPolicy{}.bindDifferingCut)
	if err != nil || len(done.Decisions) != 1 {
		t.Fatalf("done %+v, %v", done, err)
	}
	return done.Decisions[0].Found
}

func TestBindDifferingCut_BindsTheStoredISRCWhenTwoCatalogsAgreeOnAnotherLength(t *testing.T) {
	got := bindWithDifferingCut(t, caesarTrack(), map[source][]candidate{
		sourceDeezer: {caesarListing("dz", 278)},
		sourceApple:  {caesarListing("ap", 278), caesarListing("ap2", 278)},
	})
	if got.Status != statusBound || got.Reason != "bound: file length differs from catalog by 7s" || got.ISRC != "TCACR1690004" {
		t.Fatalf("got %s %q isrc %q", got.Status, got.Reason, got.ISRC)
	}
	if got.DeezerTrackID != "dz" || got.AppleSongID != "ap" || got.MBRecordingID != "" {
		t.Errorf("ids = deezer %q apple %q mb %q", got.DeezerTrackID, got.AppleSongID, got.MBRecordingID)
	}
	if cut := got.Evidence.DifferingCut; cut == nil || cut.CatalogSeconds != 278 || cut.FileSeconds != 271 {
		t.Errorf("evidence = %+v", got.Evidence.DifferingCut)
	}
}

func TestBindDifferingCut_LeavesTheTrackRefusedWhenTheCatalogsDoNotProveOneCut(t *testing.T) {
	cases := []struct {
		name     string
		listings map[source][]candidate
	}{
		{"only one catalog lists the isrc", map[source][]candidate{sourceDeezer: {caesarListing("dz", 278)}}},
		{"the catalogs list lengths further apart than the guard", map[source][]candidate{
			sourceDeezer: {caesarListing("dz", 278)}, sourceMusicBrainz: {caesarListing("mb", 282)},
		}},
		{"the catalogs agree but disagree on the title", map[source][]candidate{
			sourceDeezer: {with(caesarListing("dz", 278), func(c *candidate) { c.Title = "Get Her" })},
			sourceApple:  {with(caesarListing("ap", 278), func(c *candidate) { c.Title = "Get Her" })},
		}},
		{"the catalogs agree but credit another artist", map[source][]candidate{
			sourceDeezer: {with(caesarListing("dz", 278), func(c *candidate) { c.Artists = []string{"Jimmy Eat World"} })},
			sourceApple:  {with(caesarListing("ap", 278), func(c *candidate) { c.Artists = []string{"Jimmy Eat World"} })},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bindWithDifferingCut(t, caesarTrack(), tc.listings)
			if got.Status != statusUnbound || !strings.HasPrefix(got.Reason, reasonRefusedPrefix) || got.Evidence.DifferingCut != nil {
				t.Fatalf("got %s %q", got.Status, got.Reason)
			}
		})
	}
}

func TestBindDifferingCut_AnchorsOnTheCatalogCutClosestToTheFile(t *testing.T) {
	flock := libraryTrack{ID: "t-flock", UserID: "u1", Title: "I Ran (So Far Away)", Artist: "A Flock Of Seagulls", ISRC: "GBAHK9500304", FileSeconds: 312.84, Version: 1}
	listing := func(id string, length float64) candidate {
		return candidate{ID: id, ISRC: "GBAHK9500304", Title: "I Ran (So Far Away)", Artists: []string{"A Flock Of Seagulls"}, LengthSeconds: length}
	}
	got := bindWithDifferingCut(t, flock, map[source][]candidate{
		sourceDeezer:      {listing("dz", 295)},
		sourceMusicBrainz: {listing("mb-short", 221), listing("mb-long", 305.85)},
		sourceApple:       {listing("ap", 307)},
	})
	if got.Status != statusBound || got.MBRecordingID != "mb-long" || got.AppleSongID != "ap" || got.DeezerTrackID != "" {
		t.Fatalf("got %s %q mb %q apple %q deezer %q", got.Status, got.Reason, got.MBRecordingID, got.AppleSongID, got.DeezerTrackID)
	}
	if got.Reason != "bound: file length differs from catalog by 6s" {
		t.Errorf("reason = %q", got.Reason)
	}
}

func TestBindDifferingCut_YieldsToARecordingAtTheFilesOwnLength(t *testing.T) {
	track := caesarTrack()
	found := decide(track, []sourceOutcome{
		outcomeOf(track, sourceDeezer, []candidate{caesarListing("dz", 278)}, nil),
		outcomeOf(track, sourceApple, []candidate{caesarListing("ap", 278)}, nil),
	})
	exactCut := func(isrc string) []sourceOutcome {
		return []sourceOutcome{{Source: sourceDeezer, Candidates: []judged{ignoringISRC(track, with(caesarListing("exact", 271), func(c *candidate) { c.ISRC = isrc }))}}}
	}
	cases := []struct {
		name      string
		isrc      string
		wantBound bool
	}{
		{"another isrc the catalogs never listed for the file", "TCACR1690999", false},
		{"the stored isrc the catalogs already list", "TCACR1690004", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withRival := found
			withRival.Evidence.ArtistFirst = exactCut(tc.isrc)
			got, err := cutPolicy{}.bindDifferingCut(t.Context(), track, withRival)
			if err != nil || (got.Status == statusBound) != tc.wantBound {
				t.Fatalf("got %s %q, %v", got.Status, got.Reason, err)
			}
		})
	}
}

func TestBindDifferingCut_RefusesAGapLargerThanTheCapAndKeepsItInTheEvidence(t *testing.T) {
	track := caesarTrack()
	found := decide(track, []sourceOutcome{
		outcomeOf(track, sourceDeezer, []candidate{caesarListing("dz", 278)}, nil),
		outcomeOf(track, sourceApple, []candidate{caesarListing("ap", 278)}, nil),
	})
	cases := []struct {
		name      string
		cap       float64
		wantBound bool
	}{
		{"a gap inside the cap binds", 7, true},
		{"a gap past the cap stays refused", 6.9, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cutPolicy{maxGapSeconds: tc.cap}.bindDifferingCut(t.Context(), track, found)
			if err != nil || (got.Status == statusBound) != tc.wantBound {
				t.Fatalf("got %s %q, %v", got.Status, got.Reason, err)
			}
			if !tc.wantBound && (got.Reason != "refused-length" || got.Evidence.DifferingCut == nil || got.Evidence.DifferingCut.CatalogSeconds != 278) {
				t.Errorf("reason %q evidence %+v", got.Reason, got.Evidence.DifferingCut)
			}
		})
	}
}

func TestCutPolicyFromEnv(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		want    float64
		wantErr error
	}{
		{"unset defaults to ten seconds", nil, 10, nil},
		{"a duration", map[string]string{"LIBRARYBIND_MAX_CUT_GAP": "45s"}, 45, nil},
		{"zero", map[string]string{"LIBRARYBIND_MAX_CUT_GAP": "0s"}, 0, errBadMaxCutGap},
		{"unparsable", map[string]string{"LIBRARYBIND_MAX_CUT_GAP": "soon"}, 0, errBadMaxCutGap},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cutPolicyFromEnv(func(name string) string { return tc.env[name] })
			if !errors.Is(err, tc.wantErr) || got.maxGapSeconds != tc.want {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestBindDifferingCut_LeavesDecidedTracksAlone(t *testing.T) {
	for _, decided := range []identity{
		{Status: statusBound, Reason: reasonBoundPrefix + "deezer"},
		{Status: statusUnbound, Reason: reasonRefusedPrefix + string(guardTitle)},
		{Status: statusUnbound, Reason: reasonNoISRC},
	} {
		got, err := cutPolicy{}.bindDifferingCut(t.Context(), caesarTrack(), decided)
		if err != nil || got.Status != decided.Status || got.Reason != decided.Reason {
			t.Fatalf("%+v became %+v, %v", decided, got, err)
		}
	}
}
