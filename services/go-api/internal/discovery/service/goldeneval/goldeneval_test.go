package goldeneval

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"strings"
	"testing"
)

const (
	studioISRC  = "GBUM71029604"
	liveAidISRC = "GBUM71805979"
	blueAlbum   = "c7b245c9-8099-32ea-af95-893acedde2cf"
	greenAlbum  = "923d5ba6-7eee-3bce-bcb2-c913b2bd69d4"
	nirvanaUS   = "5b11f4ce-a62d-471e-81fc-a69a8278c7da"
	nirvanaUK   = "9282c8b4-ca0b-4c6b-b7e3-4f7762dfc4d6"
)

func goldenTrackResult(title, isrc string) domain.SearchResult {
	return domain.SearchResult{Kind: domain.ResultKindTrack, Title: title, Subtitle: "Queen", ISRC: isrc}
}

func goldenAlbumResult(title, mbid string) domain.SearchResult {
	return domain.SearchResult{Kind: domain.ResultKindAlbum, Title: title, Subtitle: "Weezer", ReleaseGroupMBID: mbid}
}

func goldenArtistResult(mbid string) domain.SearchResult {
	return domain.SearchResult{Kind: domain.ResultKindArtist, Title: "Nirvana", ArtistMBID: mbid}
}

func parseOne(t *testing.T, caseJSON string) Case {
	t.Helper()
	cases, err := Parse([]byte("[" + caseJSON + "]"))
	if err != nil {
		t.Fatalf("Parse(%s) = %v", caseJSON, err)
	}
	return cases[0]
}

const studioCaseJSON = `{"query":"queen bohemian rhapsody","kind":"track","artist":"Queen","title":"Bohemian Rhapsody","isrcs":["gbum7-1029604"],"labels":[]}`

func TestParse_RejectsWhatCouldNotBeScoredStrictly(t *testing.T) {
	tests := []struct {
		name     string
		caseJSON string
		wantErr  string
	}{
		{"unknown kind", `{"query":"q","kind":"podcast","artist":"a","title":"t","isrcs":["GBUM71029604"],"labels":[]}`, "must be track, album or artist"},
		{"track without isrc", `{"query":"q","kind":"track","artist":"a","title":"t","labels":[]}`, "identified by isrcs alone"},
		{"track with a malformed isrc", `{"query":"q","kind":"track","artist":"a","title":"t","isrcs":["NOTANISRC"],"labels":[]}`, "is not an ISRC"},
		{"album without mbid", `{"query":"q","kind":"album","artist":"a","title":"t","labels":[]}`, "lowercase mbid alone"},
		{"album with an uppercase mbid", `{"query":"q","kind":"album","artist":"a","title":"t","mbid":"C7B245C9-8099-32EA-AF95-893ACEDDE2CF","labels":[]}`, "lowercase mbid alone"},
		{"label that versiontag does not know", `{"query":"q","kind":"track","artist":"a","title":"t (Live)","isrcs":["GBUM71029604"],"labels":["concert"]}`, "not a versiontag label"},
		{"labels that disagree with the title", `{"query":"q","kind":"track","artist":"a","title":"Hello (Live)","isrcs":["GBUM71029604"],"labels":[]}`, "disagree with the labels"},
		{"known_failing that is not a ticket", `{"query":"q","kind":"track","artist":"a","title":"t","isrcs":["GBUM71029604"],"labels":[],"known_failing":"see bug"}`, "must be a ticket number"},
		{"artist with labels", `{"query":"q","kind":"artist","artist":"a","title":"a","mbid":"5b11f4ce-a62d-471e-81fc-a69a8278c7da","labels":["live"]}`, "no version labels"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte("[" + tt.caseJSON + "]"))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Parse error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestParse_RejectsDuplicateCases(t *testing.T) {
	_, err := Parse([]byte("[" + studioCaseJSON + "," + studioCaseJSON + "]"))

	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("Parse error = %v, want a duplicate-case error", err)
	}
}

func TestCase_HitsTrackByCanonicalISRCAndLabels(t *testing.T) {
	studio := parseOne(t, studioCaseJSON)
	tests := []struct {
		name   string
		result domain.SearchResult
		want   bool
	}{
		{"same ISRC in provider formatting", goldenTrackResult("Bohemian Rhapsody", "gb-um7 1029604"), true},
		{"lowercase ISRC", goldenTrackResult("Bohemian Rhapsody", "gbum71029604"), true},
		{"remaster mark is not a recording label", goldenTrackResult("Bohemian Rhapsody (2011 Remaster)", studioISRC), true},
		{"the Live Aid recording is another ISRC", goldenTrackResult("Bohemian Rhapsody (Live Aid)", liveAidISRC), false},
		{"studio ISRC under a live title is a version mismatch", goldenTrackResult("Bohemian Rhapsody (Live Aid)", studioISRC), false},
		{"right title with no ISRC proves nothing", goldenTrackResult("Bohemian Rhapsody", ""), false},
		{"an album with the same ISRC field is not a track", domain.SearchResult{Kind: domain.ResultKindAlbum, Title: "Bohemian Rhapsody", ISRC: studioISRC}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := studio.Hits(tt.result); got != tt.want {
				t.Errorf("Hits = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCase_HitsAlbumAndArtistByMBID(t *testing.T) {
	blue := parseOne(t, `{"query":"weezer blue album","kind":"album","artist":"Weezer","title":"Weezer (Blue Album)","mbid":"`+blueAlbum+`","labels":[]}`)
	us := parseOne(t, `{"query":"nirvana","kind":"artist","artist":"Nirvana","title":"Nirvana","mbid":"`+nirvanaUS+`","labels":[]}`)

	if !blue.Hits(goldenAlbumResult("Weezer", blueAlbum)) {
		t.Error("the blue album must hit on its own release-group MBID")
	}
	if blue.Hits(goldenAlbumResult("Weezer", greenAlbum)) {
		t.Error("the green album shares the title and must not hit the blue album")
	}
	if !us.Hits(goldenArtistResult(nirvanaUS)) {
		t.Error("the US band must hit on its artist MBID")
	}
	if us.Hits(goldenArtistResult(nirvanaUK)) {
		t.Error("the UK band shares the name and must not hit the US band")
	}
}

type cannedSearcher struct {
	byQuery map[string][]domain.SearchResult
	err     error
}

func (f cannedSearcher) Search(_ context.Context, query string) ([]domain.SearchResult, error) {
	return f.byQuery[query], f.err
}

func goldenSearcher(byQuery map[string][]domain.SearchResult) cannedSearcher {
	return cannedSearcher{byQuery: byQuery}
}

func TestRun_LiveAidRankedFirstIsAFailureForTheStudioSong(t *testing.T) {
	studio := parseOne(t, strings.Replace(studioCaseJSON, `"labels":[]`, `"labels":[],"within":1`, 1))
	searcher := goldenSearcher(map[string][]domain.SearchResult{
		studio.Query: {goldenTrackResult("Bohemian Rhapsody (Live Aid)", liveAidISRC), goldenTrackResult("Bohemian Rhapsody", studioISRC)},
	})

	report := Run(context.Background(), []Case{studio}, searcher, 1)

	got := report.Results[0]
	if got.Verdict != Fail || got.Position != 1 {
		t.Fatalf("verdict = %v at position %d, want a failure with the studio song at position 1", got.Verdict, got.Position)
	}
	if report.Unexpected() != 1 {
		t.Errorf("Unexpected = %d, want 1", report.Unexpected())
	}
}

func TestRun_PositionCountsOnlyResultsOfTheExpectedKind(t *testing.T) {
	studio := parseOne(t, studioCaseJSON)
	searcher := goldenSearcher(map[string][]domain.SearchResult{
		studio.Query: {goldenArtistResult(nirvanaUS), goldenAlbumResult("Queen", blueAlbum), goldenTrackResult("Bohemian Rhapsody", studioISRC)},
	})

	report := Run(context.Background(), []Case{studio}, searcher, 1)

	if got := report.Results[0]; got.Verdict != Pass || got.Position != 0 {
		t.Fatalf("verdict = %v at position %d, want pass at track position 0", got.Verdict, got.Position)
	}
}

func TestRun_KnownFailingFlipsTheRunWhenItStartsPassing(t *testing.T) {
	failing := parseOne(t, strings.Replace(studioCaseJSON, `"labels":[]`, `"labels":[],"known_failing":"#2750"`, 1))
	broken := goldenSearcher(map[string][]domain.SearchResult{failing.Query: {goldenTrackResult("Bohemian Rhapsody (Live Aid)", liveAidISRC)}})
	fixed := goldenSearcher(map[string][]domain.SearchResult{failing.Query: {goldenTrackResult("Bohemian Rhapsody", studioISRC)}})

	stillBroken := Run(context.Background(), []Case{failing}, broken, 1)
	nowFixed := Run(context.Background(), []Case{failing}, fixed, 1)

	if stillBroken.Results[0].Verdict != StillFailing || stillBroken.Unexpected() != 0 {
		t.Errorf("a still-broken known-failing case must be tolerated: verdict %v, unexpected %d", stillBroken.Results[0].Verdict, stillBroken.Unexpected())
	}
	if nowFixed.Results[0].Verdict != Fixed || nowFixed.Unexpected() != 1 {
		t.Errorf("a fixed known-failing case must fail the run until its marker is removed: verdict %v, unexpected %d", nowFixed.Results[0].Verdict, nowFixed.Unexpected())
	}
}

func TestRun_AnErroredQueryIsAFailureEvenWhenKnownFailing(t *testing.T) {
	failing := parseOne(t, strings.Replace(studioCaseJSON, `"labels":[]`, `"labels":[],"known_failing":"#2750"`, 1))
	searcher := cannedSearcher{err: errors.New("provider down")}

	report := Run(context.Background(), []Case{failing}, searcher, 1)

	got := report.Results[0]
	if got.Verdict != Errored || got.Error == "" || report.Unexpected() != 1 {
		t.Fatalf("verdict = %v, error %q, unexpected %d, want an errored failure that known_failing cannot absorb", got.Verdict, got.Error, report.Unexpected())
	}
}

func TestReport_MetricsCountKnownFailingAsMisses(t *testing.T) {
	studio := parseOne(t, studioCaseJSON)
	failing := parseOne(t, `{"query":"queen live aid","kind":"track","artist":"Queen","title":"Bohemian Rhapsody (Live Aid)","isrcs":["GBUM71805979"],"labels":["live"],"known_failing":"#2750"}`)
	searcher := goldenSearcher(map[string][]domain.SearchResult{
		studio.Query:  {goldenTrackResult("Bohemian Rhapsody", studioISRC)},
		failing.Query: {goldenTrackResult("Bohemian Rhapsody", studioISRC)},
	})

	report := Run(context.Background(), []Case{studio, failing}, searcher, 1)

	metrics := map[string]float64{}
	for _, m := range report.Metrics() {
		metrics[m.Name] = m.Value
	}
	if metrics["golden.hit_rate"] != 0.5 || metrics["golden.top1_rate"] != 0.5 || metrics["golden.unexpected_cases"] != 0 {
		t.Errorf("metrics = %v, want hit 0.5, top1 0.5, unexpected 0", metrics)
	}
}
