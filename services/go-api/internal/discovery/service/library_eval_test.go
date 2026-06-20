package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// fakeSearcher returns canned results keyed by query; no live APIs.
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

func track(title, artist string) domain.SearchResult {
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
		wantOutcome EvalOutcome
		wantTopKind string // only checked when outcome is fail_wrong_top
	}{
		{
			name:        "entity at position 0 passes",
			entity:      entity,
			results:     []domain.SearchResult{track("HUMBLE.", "Kendrick Lamar"), album("DAMN.", "Kendrick Lamar")},
			wantOutcome: EvalPass,
		},
		{
			name:        "entity below position 0 fails with what beat it",
			entity:      entity,
			results:     []domain.SearchResult{album("DAMN.", "Kendrick Lamar"), track("HUMBLE.", "Kendrick Lamar")},
			wantOutcome: EvalFailWrongTop,
			wantTopKind: "album",
		},
		{
			name:        "case-insensitive title and artist still match",
			entity:      entity,
			results:     []domain.SearchResult{track("humble.", "kendrick lamar")},
			wantOutcome: EvalPass,
		},
		{
			name:        "empty results fail as no-results",
			entity:      entity,
			results:     []domain.SearchResult{},
			wantOutcome: EvalFailNoResults,
		},
		{
			name:        "search error fails as no-results",
			entity:      entity,
			searchErr:   errors.New("provider down"),
			wantOutcome: EvalFailNoResults,
		},
		{
			name:        "empty artist is skipped",
			entity:      LibraryEntity{Title: "Untitled", Artist: "   "},
			wantOutcome: EvalSkipped,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			searcher := &fakeSearcher{
				byQuery: map[string][]domain.SearchResult{query: tt.results},
				err:     tt.searchErr,
			}
			got := evalOne(context.Background(), tt.entity, searcher)

			if got.Outcome != tt.wantOutcome {
				t.Fatalf("outcome = %v, want %v", got.Outcome, tt.wantOutcome)
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

func TestRunLibraryEval_Aggregation(t *testing.T) {
	entities := []LibraryEntity{
		{Title: "HUMBLE.", Artist: "Kendrick Lamar"}, // pass
		{Title: "Circles", Artist: "Post Malone"},    // fail: album on top
		{Title: "Ghost Track", Artist: "Nobody"},     // fail: no results
		{Title: "Orphan", Artist: ""},                // skipped
	}
	searcher := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Kendrick Lamar HUMBLE.": {track("HUMBLE.", "Kendrick Lamar")},
		"Post Malone Circles":    {album("Circles", "Post Malone"), track("Circles", "Post Malone")},
		"Nobody Ghost Track":     {},
	}}

	report := RunLibraryEval(context.Background(), entities, searcher, 2)

	if report.Total != 4 {
		t.Errorf("Total = %d, want 4", report.Total)
	}
	if report.Evaluated != 3 {
		t.Errorf("Evaluated = %d, want 3", report.Evaluated)
	}
	if report.Passed != 1 {
		t.Errorf("Passed = %d, want 1", report.Passed)
	}
	if report.Failed != 2 {
		t.Errorf("Failed = %d, want 2", report.Failed)
	}
	if report.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", report.Skipped)
	}
	if report.FailuresByTopKind["album"] != 1 {
		t.Errorf("FailuresByTopKind[album] = %d, want 1", report.FailuresByTopKind["album"])
	}
	if report.FailuresByTopKind["none"] != 1 {
		t.Errorf("FailuresByTopKind[none] = %d, want 1", report.FailuresByTopKind["none"])
	}
	if got := report.PassRate(); got < 0.33 || got > 0.34 {
		t.Errorf("PassRate = %.4f, want ~0.3333", got)
	}
}
