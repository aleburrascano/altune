package eval_test

import (
	"altune/go-api/internal/discovery"
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeSearcher struct {
	byQuery map[string][]discovery.SearchResult
	err     error
}

func (f *fakeSearcher) Search(_ context.Context, query string) ([]discovery.SearchResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byQuery[query], nil
}

type answersEveryQuery []discovery.SearchResult

func (a answersEveryQuery) Search(context.Context, string) ([]discovery.SearchResult, error) {
	return a, nil
}

func evalOne(t *testing.T, entity discovery.LibraryEntity, searcher discovery.Searcher, k int) discovery.EvalResult {
	t.Helper()
	report := discovery.RunLibraryEvalMode(context.Background(), []discovery.LibraryEntity{entity}, searcher, 1, k, discovery.QueryExact, nil)
	if len(report.Results) != 1 {
		t.Fatalf("got %d results, want 1", len(report.Results))
	}
	return report.Results[0]
}

func matchesEntity(t *testing.T, result discovery.SearchResult, entity discovery.LibraryEntity) bool {
	t.Helper()
	return evalOne(t, entity, answersEveryQuery{result}, 1).Outcome == discovery.EvalPass
}

func evalTrack(title, artist string) discovery.SearchResult {
	return discovery.SearchResult{Kind: discovery.ResultKindTrack, Title: title, Subtitle: artist}
}

func album(title, artist string) discovery.SearchResult {
	return discovery.SearchResult{Kind: discovery.ResultKindAlbum, Title: title, Subtitle: artist}
}

func TestEvalOne(t *testing.T) {
	entity := discovery.LibraryEntity{Title: "HUMBLE.", Artist: "Kendrick Lamar"}
	query := "Kendrick Lamar HUMBLE."

	tests := []struct {
		name        string
		entity      discovery.LibraryEntity
		results     []discovery.SearchResult
		searchErr   error
		k           int
		wantOutcome discovery.EvalOutcome
		wantPos     int
		wantTopKind string
	}{
		{
			name:        "entity at #1 passes top-1",
			entity:      entity,
			results:     []discovery.SearchResult{evalTrack("HUMBLE.", "Kendrick Lamar"), album("DAMN.", "Kendrick Lamar")},
			k:           1,
			wantOutcome: discovery.EvalPass,
			wantPos:     0,
		},
		{
			name:        "entity below #1 fails at k=1",
			entity:      entity,
			results:     []discovery.SearchResult{album("DAMN.", "Kendrick Lamar"), evalTrack("HUMBLE.", "Kendrick Lamar")},
			k:           1,
			wantOutcome: discovery.EvalFailWrongTop,
			wantTopKind: "album",
		},
		{
			name:        "entity below #1 passes within top-3",
			entity:      entity,
			results:     []discovery.SearchResult{album("DAMN.", "Kendrick Lamar"), evalTrack("HUMBLE.", "Kendrick Lamar")},
			k:           3,
			wantOutcome: discovery.EvalPass,
			wantPos:     1,
		},
		{
			name:        "case-insensitive title and artist still match",
			entity:      entity,
			results:     []discovery.SearchResult{evalTrack("humble.", "kendrick lamar")},
			k:           3,
			wantOutcome: discovery.EvalPass,
			wantPos:     0,
		},
		{
			name:        "empty results fail as no-results",
			entity:      entity,
			results:     []discovery.SearchResult{},
			k:           3,
			wantOutcome: discovery.EvalFailNoResults,
		},
		{
			name:        "search error fails as no-results",
			entity:      entity,
			searchErr:   errors.New("provider down"),
			k:           3,
			wantOutcome: discovery.EvalFailNoResults,
		},
		{
			name:        "empty artist is skipped",
			entity:      discovery.LibraryEntity{Title: "Untitled", Artist: "   "},
			k:           3,
			wantOutcome: discovery.EvalSkipped,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			searcher := &fakeSearcher{
				byQuery: map[string][]discovery.SearchResult{query: tt.results},
				err:     tt.searchErr,
			}
			got := evalOne(t, tt.entity, searcher, tt.k)

			if got.Outcome != tt.wantOutcome {
				t.Fatalf("outcome = %v, want %v", got.Outcome, tt.wantOutcome)
			}
			if tt.wantOutcome == discovery.EvalPass && got.MatchPosition != tt.wantPos {
				t.Errorf("MatchPosition = %d, want %d", got.MatchPosition, tt.wantPos)
			}
			if tt.wantOutcome == discovery.EvalFailWrongTop {
				if got.Top == nil {
					t.Fatalf("expected Top to be recorded on wrong-top failure")
				}
				if got.Top.Kind != tt.wantTopKind {
					t.Errorf("Top.Kind = %q, want %q", got.Top.Kind, tt.wantTopKind)
				}
			}
			if tt.searchErr != nil && got.Error == "" {
				t.Errorf("expected search error to be recorded")
			}
		})
	}
}

func TestMatchesEntity(t *testing.T) {
	tests := []struct {
		name   string
		result discovery.SearchResult
		entity discovery.LibraryEntity
		want   bool
	}{
		{"exact title and artist", evalTrack("HUMBLE.", "Kendrick Lamar"), discovery.LibraryEntity{Title: "HUMBLE.", Artist: "Kendrick Lamar"}, true},
		{"artist prefix embedded in title", evalTrack("A-Ha - Take On Me", "a-ha"), discovery.LibraryEntity{Title: "Take On Me", Artist: "a-ha"}, true},
		{"track-number prefix in title", evalTrack("07-The Best Was Yet To Come", "Bryan Adams"), discovery.LibraryEntity{Title: "The Best Was Yet To Come", Artist: "Bryan Adams"}, true},
		{"reuploader subtitle but artist in title", discovery.SearchResult{Kind: discovery.ResultKindTrack, Title: "Lil Tecca - Yup", Subtitle: "lost_files"}, discovery.LibraryEntity{Title: "Yup", Artist: "Lil Tecca"}, true},
		{"short title not substring-matched", evalTrack("Going Home", "Drake"), discovery.LibraryEntity{Title: "Go", Artist: "Drake"}, false},
		{"different track same artist", evalTrack("DAMN.", "Kendrick Lamar"), discovery.LibraryEntity{Title: "HUMBLE.", Artist: "Kendrick Lamar"}, false},
		{"album is never a track match", album("Circles", "Post Malone"), discovery.LibraryEntity{Title: "Circles", Artist: "Post Malone"}, false},
		{"symbol-only artist matches via subtitle", discovery.SearchResult{Kind: discovery.ResultKindTrack, Title: "CARNIVAL", Subtitle: "¥$"}, discovery.LibraryEntity{Title: "CARNIVAL", Artist: "¥$"}, true},
		{"symbol-only artist rejects wrong result", discovery.SearchResult{Kind: discovery.ResultKindTrack, Title: "CARNIVAL", Subtitle: "Some Cover Band"}, discovery.LibraryEntity{Title: "CARNIVAL", Artist: "¥$"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesEntity(t, tt.result, tt.entity); got != tt.want {
				t.Errorf("matchesEntity = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunLibraryEvalMode_Aggregation(t *testing.T) {
	entities := []discovery.LibraryEntity{
		{Title: "HUMBLE.", Artist: "Kendrick Lamar"},
		{Title: "Circles", Artist: "Post Malone"},
		{Title: "Ghost Track", Artist: "Nobody"},
		{Title: "Orphan", Artist: ""},
	}
	searcher := &fakeSearcher{byQuery: map[string][]discovery.SearchResult{
		"Kendrick Lamar HUMBLE.": {evalTrack("HUMBLE.", "Kendrick Lamar")},
		"Post Malone Circles":    {album("Circles", "Post Malone"), evalTrack("Circles", "Post Malone")},
		"Nobody Ghost Track":     {},
	}}

	report := discovery.RunLibraryEvalMode(context.Background(), entities, searcher, 2, 3, discovery.QueryExact, nil)

	if report.K != 3 {
		t.Errorf("K = %d, want 3", report.K)
	}
	if report.Total != 4 {
		t.Errorf("Total = %d, want 4", report.Total)
	}
	if report.Evaluated != 3 {
		t.Errorf("Evaluated = %d, want 3", report.Evaluated)
	}
	if report.Top1Passed != 1 {
		t.Errorf("Top1Passed = %d, want 1", report.Top1Passed)
	}
	if report.TopKPassed != 2 {
		t.Errorf("TopKPassed = %d, want 2", report.TopKPassed)
	}
	if report.Failed != 1 {
		t.Errorf("Failed = %d, want 1", report.Failed)
	}
	if report.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", report.Skipped)
	}
	if report.FailuresByTopKind["none"] != 1 {
		t.Errorf("FailuresByTopKind[none] = %d, want 1", report.FailuresByTopKind["none"])
	}
	if got := report.Top1Rate(); got < 0.33 || got > 0.34 {
		t.Errorf("Top1Rate = %.4f, want ~0.3333", got)
	}
	if got := report.TopKRate(); got < 0.66 || got > 0.67 {
		t.Errorf("TopKRate = %.4f, want ~0.6667", got)
	}
}

func TestEvalOutcome_StringAndJSON(t *testing.T) {
	tests := []struct {
		o    discovery.EvalOutcome
		want string
	}{
		{discovery.EvalPass, "pass"},
		{discovery.EvalFailWrongTop, "fail_wrong_top"},
		{discovery.EvalFailNoResults, "fail_no_results"},
		{discovery.EvalSkipped, "skipped"},
		{discovery.EvalOutcomeUnknown, "unknown"},
	}
	for _, tt := range tests {
		if got := tt.o.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
		b, err := tt.o.MarshalJSON()
		if err != nil || string(b) != `"`+tt.want+`"` {
			t.Errorf("MarshalJSON = %s (%v), want %q", b, err, tt.want)
		}
	}
}

func TestRunLibraryEvalMode_EachModeSearchesItsOwnQueryAndLabelsItsCorpus(t *testing.T) {
	e := discovery.LibraryEntity{Title: "HUMBLE.", Artist: "Kendrick Lamar"}
	cases := []struct {
		mode       discovery.QueryMode
		wantQuery  string
		wantCorpus string
	}{
		{discovery.QueryExact, "Kendrick Lamar HUMBLE.", ""},
		{discovery.QueryTitleOnly, "HUMBLE.", "hard"},
	}
	for _, tc := range cases {
		r := discovery.RunLibraryEvalMode(context.Background(), []discovery.LibraryEntity{e}, &fakeSearcher{}, 1, 3, tc.mode, nil)
		if len(r.Results) != 1 || r.Results[0].Query != tc.wantQuery {
			t.Fatalf("mode %v results = %+v, want one result for query %q", tc.mode, r.Results, tc.wantQuery)
		}
		if r.Corpus != tc.wantCorpus {
			t.Errorf("mode %v corpus = %q, want %q", tc.mode, r.Corpus, tc.wantCorpus)
		}
	}
}

func TestRunLibraryEvalMode_AllSkippedReportsZeroRates(t *testing.T) {
	entities := []discovery.LibraryEntity{
		{Title: "One", Artist: ""},
		{Title: "Two", Artist: "   "},
	}
	report := discovery.RunLibraryEvalMode(context.Background(), entities, &fakeSearcher{}, 1, 3, discovery.QueryTitleOnly, nil)
	if report.Evaluated != 0 || report.Skipped != 2 {
		t.Fatalf("Evaluated/Skipped = %d/%d, want 0/2", report.Evaluated, report.Skipped)
	}
	if report.Top1Rate() != 0 || report.TopKRate() != 0 {
		t.Error("all-skipped run must report 0 rates, not NaN")
	}
	if report.Corpus != "hard" {
		t.Errorf("Corpus = %q, want hard", report.Corpus)
	}
}

func TestOutcomeEnumsMarshalJSON(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"ArtistIntentUnknown", discovery.ArtistIntentUnknown, `"unknown"`},
		{"ArtistIntentPass", discovery.ArtistIntentPass, `"pass"`},
		{"ArtistIntentBuried", discovery.ArtistIntentBuried, `"buried"`},
		{"ArtistIntentBelowK", discovery.ArtistIntentBelowK, `"below_k"`},
		{"ArtistIntentAbsent", discovery.ArtistIntentAbsent, `"absent"`},
		{"ArtistIntentNoResults", discovery.ArtistIntentNoResults, `"no_results"`},
		{"ArtistIntentSkipped", discovery.ArtistIntentSkipped, `"skipped"`},
		{"ArtistIntentOutOfRange", discovery.ArtistIntentOutcome(99), `"unknown"`},
		{"GapStrengthUnknown", discovery.GapStrengthUnknown, `"unknown"`},
		{"GapStrong", discovery.GapStrong, `"strong"`},
		{"GapWeak", discovery.GapWeak, `"weak"`},
		{"GapAbandoned", discovery.GapAbandoned, `"abandoned"`},
		{"GapStrengthOutOfRange", discovery.GapStrength(99), `"unknown"`},
		{"EvalOutcomeUnknown", discovery.EvalOutcomeUnknown, `"unknown"`},
		{"EvalPass", discovery.EvalPass, `"pass"`},
		{"EvalFailWrongTop", discovery.EvalFailWrongTop, `"fail_wrong_top"`},
		{"EvalFailNoResults", discovery.EvalFailNoResults, `"fail_no_results"`},
		{"EvalSkipped", discovery.EvalSkipped, `"skipped"`},
		{"EvalOutcomeOutOfRange", discovery.EvalOutcome(99), `"unknown"`},
		{"EmbeddedInStruct", struct {
			A discovery.ArtistIntentOutcome `json:"a"`
			G discovery.GapStrength         `json:"g"`
			E discovery.EvalOutcome         `json:"e"`
		}{discovery.ArtistIntentBelowK, discovery.GapWeak, discovery.EvalFailWrongTop}, `{"a":"below_k","g":"weak","e":"fail_wrong_top"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestMatchesEntity_VersionLabelsAndKnownRecording(t *testing.T) {
	studio := discovery.LibraryEntity{Title: "Bohemian Rhapsody", Artist: "Queen"}
	known := discovery.LibraryEntity{Title: "Bohemian Rhapsody", Artist: "Queen", Known: &discovery.KnownRecording{ISRCs: []string{"GBUM71029604"}}}
	withISRC := func(title, isrc string) discovery.SearchResult {
		r := evalTrack(title, "Queen")
		r.ISRC = isrc
		return r
	}
	tests := []struct {
		name   string
		result discovery.SearchResult
		entity discovery.LibraryEntity
		want   bool
	}{
		{"the Live Aid recording is not the studio song", evalTrack("Bohemian Rhapsody (Live Aid)", "Queen"), studio, false},
		{"a live entity matches its live recording", evalTrack("Bohemian Rhapsody (Live Aid)", "Queen"), discovery.LibraryEntity{Title: "Bohemian Rhapsody (Live Aid)", Artist: "Queen"}, true},
		{"known ISRC in provider formatting matches", withISRC("Bohemian Rhapsody", "gb-um7-10-29604"), known, true},
		{"right title and artist but another ISRC does not match", withISRC("Bohemian Rhapsody", "GBUM71805979"), known, false},
		{"right title and artist with no ISRC does not match a known recording", withISRC("Bohemian Rhapsody", ""), known, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesEntity(t, tt.result, tt.entity); got != tt.want {
				t.Errorf("matchesEntity = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunLibraryEvalMode_OnlyAPassAtTheTopCountsAsTop1(t *testing.T) {
	entity := discovery.LibraryEntity{Title: "Circles", Artist: "Post Malone"}
	for _, tc := range []struct {
		name    string
		results []discovery.SearchResult
		top1    int
	}{
		{"match at rank 0", []discovery.SearchResult{evalTrack("Circles", "Post Malone")}, 1},
		{"match at rank 1", []discovery.SearchResult{album("Circles", "Post Malone"), evalTrack("Circles", "Post Malone")}, 0},
	} {
		searcher := &fakeSearcher{byQuery: map[string][]discovery.SearchResult{"Post Malone Circles": tc.results}}
		r := discovery.RunLibraryEvalMode(context.Background(), []discovery.LibraryEntity{entity}, searcher, 1, 3, discovery.QueryExact, nil)
		if r.TopKPassed != 1 || r.Top1Passed != tc.top1 {
			t.Errorf("%s: topK/top1 = %d/%d, want 1/%d", tc.name, r.TopKPassed, r.Top1Passed, tc.top1)
		}
	}
}
