package eval

import (
	"context"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// Reuses fakeSearcher (library_eval_test.go) and track (merge_test.go). The
// track helper sets ExternalID = "<title>:<provider>".

func TestRunMergeEval_clean(t *testing.T) {
	// One result for the query — nothing to under-merge.
	entities := []LibraryEntity{{Title: "Humble", Artist: "Kendrick"}}
	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Kendrick Humble": {track("Humble", "Kendrick", domain.ProviderDeezer, nil)},
	}}
	r := RunMergeEval(context.Background(), entities, fake, 1, nil)
	if r.UnderMergeIncidents != 0 || r.UnderMergeRate() != 0 {
		t.Fatalf("expected zero under-merge, got incidents=%d rate=%v", r.UnderMergeIncidents, r.UnderMergeRate())
	}
	if r.Evaluated != 1 || r.CleanQueries != 1 {
		t.Errorf("expected 1 clean evaluated query, got evaluated=%d clean=%d", r.Evaluated, r.CleanQueries)
	}
}

func TestRunMergeEval_underMergeIdenticalCanonical(t *testing.T) {
	// Two rows with identical canonical title+artist from different providers —
	// merge's contract says collapse; left separate = a provable under-merge.
	entities := []LibraryEntity{{Title: "Humble", Artist: "Kendrick"}}
	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Kendrick Humble": {
			track("Humble", "Kendrick", domain.ProviderDeezer, nil),
			track("Humble", "Kendrick", domain.ProviderITunes, nil),
		},
	}}
	r := RunMergeEval(context.Background(), entities, fake, 1, nil)
	if r.UnderMergeIncidents != 1 {
		t.Fatalf("expected 1 under-merge incident, got %d", r.UnderMergeIncidents)
	}
	if r.ResultsSeen != 2 || r.UnderMergeRate() != 0.5 {
		t.Errorf("under_merge_rate = %v (incidents %d / seen %d), want 0.5", r.UnderMergeRate(), r.UnderMergeIncidents, r.ResultsSeen)
	}
	if len(r.Failures()) != 1 || r.Failures()[0].Reason != "under_merge" {
		t.Errorf("expected one under_merge failure, got %+v", r.Failures())
	}
}

func TestRunMergeEval_distinctVersionsNotUnderMerge(t *testing.T) {
	// Same title core but a non-bracketed version marker survives normalization,
	// so these are DIFFERENT canonical titles — correctly NOT an under-merge.
	entities := []LibraryEntity{{Title: "Redrum", Artist: "21 Savage"}}
	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"21 Savage Redrum": {
			track("Redrum", "21 Savage", domain.ProviderSoundCloud, nil),
			track("Redrum sped up", "21 Savage", domain.ProviderSoundCloud, nil),
			track("Redrum slowed", "21 Savage", domain.ProviderSoundCloud, nil),
		},
	}}
	r := RunMergeEval(context.Background(), entities, fake, 1, nil)
	if r.UnderMergeIncidents != 0 {
		t.Errorf("distinct versions must not count as under-merge, got %d incidents", r.UnderMergeIncidents)
	}
}

func TestRunMergeEval_underMergeByISRC(t *testing.T) {
	// Different titles but the SAME isrc — provably one recording; left apart = bug.
	entities := []LibraryEntity{{Title: "Humble", Artist: "Kendrick"}}
	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Kendrick Humble": {
			track("HUMBLE.", "Kendrick", domain.ProviderDeezer, map[string]any{"isrc": "USUM71703089"}),
			track("Humble (Clean)", "Kendrick", domain.ProviderITunes, map[string]any{"isrc": "USUM71703089"}),
		},
	}}
	r := RunMergeEval(context.Background(), entities, fake, 1, nil)
	if r.UnderMergeIncidents != 1 {
		t.Errorf("shared ISRC across two rows must be an under-merge, got %d", r.UnderMergeIncidents)
	}
}

func TestRunMergeEval_overMerge(t *testing.T) {
	entities := []LibraryEntity{
		{Title: "Love", Artist: "Artist"},
		{Title: "Story", Artist: "Artist"},
	}
	merged := track("Love Story", "Artist", domain.ProviderDeezer, nil)
	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Artist Love":  {merged},
		"Artist Story": {merged},
	}}
	r := RunMergeEval(context.Background(), entities, fake, 1, nil)
	if r.OverMerged != 1 || r.OverMergeRate() != 1.0 {
		t.Fatalf("expected over-merge rate 1.0, got over=%d rate=%v", r.OverMerged, r.OverMergeRate())
	}
}

func TestRunMergeEval_noMatchExcluded(t *testing.T) {
	entities := []LibraryEntity{
		{Title: "Found", Artist: "Artist"},
		{Title: "Missing", Artist: "Artist"},
	}
	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Artist Found": {track("Found", "Artist", domain.ProviderDeezer, nil)},
	}}
	r := RunMergeEval(context.Background(), entities, fake, 1, nil)
	if r.NoMatch != 1 || r.Evaluated != 1 {
		t.Errorf("coverage miss handling wrong: no_match=%d evaluated=%d", r.NoMatch, r.Evaluated)
	}
}
