package eval

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"testing"
)

type fakeSearcher struct {
	byQuery map[string][]domain.SearchResult
	err     error
}

func (f *fakeSearcher) Search(_ context.Context, query string) ([]domain.SearchResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byQuery[query], nil
}

func evalTrack(title, artist string) domain.SearchResult {
	return domain.SearchResult{Kind: domain.ResultKindTrack, Title: title, Subtitle: artist}
}

func album(title, artist string) domain.SearchResult {
	return domain.SearchResult{Kind: domain.ResultKindAlbum, Title: title, Subtitle: artist}
}

func TestEvalOne(t *testing.T) {
	entity := LibraryEntity{Title: "HUMBLE.", Artist: "Kendrick Lamar"}
	query := "Kendrick Lamar HUMBLE."

	tests := []struct {
		name        string
		entity      LibraryEntity
		results     []domain.SearchResult
		searchErr   error
		k           int
		wantOutcome EvalOutcome
		wantPos     int
		wantTopKind string
	}{
		{
			name:        "entity at #1 passes top-1",
			entity:      entity,
			results:     []domain.SearchResult{evalTrack("HUMBLE.", "Kendrick Lamar"), album("DAMN.", "Kendrick Lamar")},
			k:           1,
			wantOutcome: EvalPass,
			wantPos:     0,
		},
		{
			name:        "entity below #1 fails at k=1",
			entity:      entity,
			results:     []domain.SearchResult{album("DAMN.", "Kendrick Lamar"), evalTrack("HUMBLE.", "Kendrick Lamar")},
			k:           1,
			wantOutcome: EvalFailWrongTop,
			wantTopKind: "album",
		},
		{
			name:        "entity below #1 passes within top-3",
			entity:      entity,
			results:     []domain.SearchResult{album("DAMN.", "Kendrick Lamar"), evalTrack("HUMBLE.", "Kendrick Lamar")},
			k:           3,
			wantOutcome: EvalPass,
			wantPos:     1,
		},
		{
			name:        "case-insensitive title and artist still match",
			entity:      entity,
			results:     []domain.SearchResult{evalTrack("humble.", "kendrick lamar")},
			k:           3,
			wantOutcome: EvalPass,
			wantPos:     0,
		},
		{
			name:        "empty results fail as no-results",
			entity:      entity,
			results:     []domain.SearchResult{},
			k:           3,
			wantOutcome: EvalFailNoResults,
		},
		{
			name:        "search error fails as no-results",
			entity:      entity,
			searchErr:   errors.New("provider down"),
			k:           3,
			wantOutcome: EvalFailNoResults,
		},
		{
			name:        "empty artist is skipped",
			entity:      LibraryEntity{Title: "Untitled", Artist: "   "},
			k:           3,
			wantOutcome: EvalSkipped,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			searcher := &fakeSearcher{
				byQuery: map[string][]domain.SearchResult{query: tt.results},
				err:     tt.searchErr,
			}
			got := evalOneQuery(context.Background(), query, tt.entity, searcher, tt.k)

			if got.Outcome != tt.wantOutcome {
				t.Fatalf("outcome = %v, want %v", got.Outcome, tt.wantOutcome)
			}
			if tt.wantOutcome == EvalPass && got.MatchPosition != tt.wantPos {
				t.Errorf("MatchPosition = %d, want %d", got.MatchPosition, tt.wantPos)
			}
			if tt.wantOutcome == EvalFailWrongTop {
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
		result domain.SearchResult
		entity LibraryEntity
		want   bool
	}{
		{"exact title and artist", evalTrack("HUMBLE.", "Kendrick Lamar"), LibraryEntity{Title: "HUMBLE.", Artist: "Kendrick Lamar"}, true},
		{"artist prefix embedded in title", evalTrack("A-Ha - Take On Me", "a-ha"), LibraryEntity{Title: "Take On Me", Artist: "a-ha"}, true},
		{"track-number prefix in title", evalTrack("07-The Best Was Yet To Come", "Bryan Adams"), LibraryEntity{Title: "The Best Was Yet To Come", Artist: "Bryan Adams"}, true},
		{"reuploader subtitle but artist in title", domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Lil Tecca - Yup", Subtitle: "lost_files"}, LibraryEntity{Title: "Yup", Artist: "Lil Tecca"}, true},
		{"short title not substring-matched", evalTrack("Going Home", "Drake"), LibraryEntity{Title: "Go", Artist: "Drake"}, false},
		{"different track same artist", evalTrack("DAMN.", "Kendrick Lamar"), LibraryEntity{Title: "HUMBLE.", Artist: "Kendrick Lamar"}, false},
		{"album is never a track match", album("Circles", "Post Malone"), LibraryEntity{Title: "Circles", Artist: "Post Malone"}, false},
		{"symbol-only artist matches via subtitle", domain.SearchResult{Kind: domain.ResultKindTrack, Title: "CARNIVAL", Subtitle: "¥$"}, LibraryEntity{Title: "CARNIVAL", Artist: "¥$"}, true},
		{"symbol-only artist rejects wrong result", domain.SearchResult{Kind: domain.ResultKindTrack, Title: "CARNIVAL", Subtitle: "Some Cover Band"}, LibraryEntity{Title: "CARNIVAL", Artist: "¥$"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesEntity(tt.result, tt.entity); got != tt.want {
				t.Errorf("matchesEntity = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunLibraryEvalMode_Aggregation(t *testing.T) {
	entities := []LibraryEntity{
		{Title: "HUMBLE.", Artist: "Kendrick Lamar"},
		{Title: "Circles", Artist: "Post Malone"},
		{Title: "Ghost Track", Artist: "Nobody"},
		{Title: "Orphan", Artist: ""},
	}
	searcher := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Kendrick Lamar HUMBLE.": {evalTrack("HUMBLE.", "Kendrick Lamar")},
		"Post Malone Circles":    {album("Circles", "Post Malone"), evalTrack("Circles", "Post Malone")},
		"Nobody Ghost Track":     {},
	}}

	report := RunLibraryEvalMode(context.Background(), entities, searcher, 2, 3, QueryExact, nil)

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
		o    EvalOutcome
		want string
	}{
		{EvalPass, "pass"},
		{EvalFailWrongTop, "fail_wrong_top"},
		{EvalFailNoResults, "fail_no_results"},
		{EvalSkipped, "skipped"},
		{EvalOutcomeUnknown, "unknown"},
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

func TestQueryMode_QueryForAndLabel(t *testing.T) {
	e := LibraryEntity{Title: "HUMBLE.", Artist: "Kendrick Lamar"}
	if got := QueryExact.queryFor(e); got != "Kendrick Lamar HUMBLE." {
		t.Errorf("exact query = %q", got)
	}
	if got := QueryTitleOnly.queryFor(e); got != "HUMBLE." {
		t.Errorf("title-only query = %q", got)
	}
	if QueryExact.label() != "" || QueryTitleOnly.label() != "hard" {
		t.Errorf("labels = %q/%q, want \"\"/hard", QueryExact.label(), QueryTitleOnly.label())
	}
}

func TestRunLibraryEvalMode_AllSkippedReportsZeroRates(t *testing.T) {
	entities := []LibraryEntity{
		{Title: "One", Artist: ""},
		{Title: "Two", Artist: "   "},
	}
	report := RunLibraryEvalMode(context.Background(), entities, &fakeSearcher{}, 1, 3, QueryTitleOnly, nil)
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
