package eval_test

import (
	"altune/go-api/internal/discovery"
	"context"
	"testing"
)

type fakeVariantSearcher struct {
	with    map[string][]discovery.SearchResult
	without map[string][]discovery.SearchResult
}

func (f *fakeVariantSearcher) SearchVariants(_ context.Context, q string) ([]discovery.SearchResult, []discovery.SearchResult) {
	return f.with[q], f.without[q]
}

func TestRunDiversityEvalMode_costWhenReshapeDemotesTarget(t *testing.T) {
	entity := discovery.LibraryEntity{Title: "Humble", Artist: "Kendrick"}
	q := "Kendrick Humble"

	other := track("Other", "Kendrick", discovery.ProviderDeezer, nil)
	target := track("Humble", "Kendrick", discovery.ProviderDeezer, nil)
	fake := &fakeVariantSearcher{
		without: map[string][]discovery.SearchResult{q: {target, other}},
		with:    map[string][]discovery.SearchResult{q: {other, other, target}},
	}
	r := discovery.RunDiversityEvalMode(context.Background(), []discovery.LibraryEntity{entity}, fake, 1, 2, discovery.QueryExact, nil)

	if r.LostToReshape != 1 {
		t.Fatalf("expected 1 lost to reshape, got %d", r.LostToReshape)
	}
	if r.CostRate() != 1.0 {
		t.Errorf("cost_rate = %v, want 1.0", r.CostRate())
	}
	if len(r.Failures()) != 1 || r.Failures()[0].Reason != "lost_to_reshape" {
		t.Errorf("expected one lost_to_reshape failure, got %+v", r.Failures())
	}
}

func TestRunDiversityEvalMode_noCostWhenTargetSurvives(t *testing.T) {
	entity := discovery.LibraryEntity{Title: "Humble", Artist: "Kendrick"}
	q := "Kendrick Humble"
	target := track("Humble", "Kendrick", discovery.ProviderDeezer, nil)
	fake := &fakeVariantSearcher{
		without: map[string][]discovery.SearchResult{q: {target}},
		with:    map[string][]discovery.SearchResult{q: {target}},
	}
	r := discovery.RunDiversityEvalMode(context.Background(), []discovery.LibraryEntity{entity}, fake, 1, 3, discovery.QueryExact, nil)
	if r.Evaluated != 1 {
		t.Fatalf("Evaluated = %d, want 1", r.Evaluated)
	}
	if r.LostToReshape != 0 || r.CostRate() != 0 {
		t.Errorf("target survived reshape but cost reported: lost=%d rate=%v", r.LostToReshape, r.CostRate())
	}
}

func TestRunDiversityEvalMode_ConcentrationIsTheSumOfSquaredArtistSharesInTheTopK(t *testing.T) {
	entity := discovery.LibraryEntity{Title: "A", Artist: "Drake"}
	fake := &fakeVariantSearcher{
		with: map[string][]discovery.SearchResult{"Drake A": {
			track("A", "Drake", discovery.ProviderDeezer, nil),
			track("B", "Drake", discovery.ProviderDeezer, nil),
			track("C", "Drake", discovery.ProviderDeezer, nil),
		}},
		without: map[string][]discovery.SearchResult{"Drake A": {
			track("A", "Drake", discovery.ProviderDeezer, nil),
			track("B", "Adele", discovery.ProviderDeezer, nil),
			track("C", "Queen", discovery.ProviderDeezer, nil),
		}},
	}

	r := discovery.RunDiversityEvalMode(context.Background(), []discovery.LibraryEntity{entity}, fake, 1, 3, discovery.QueryExact, nil)

	if r.ConcentrationWith != 1.0 {
		t.Errorf("all-one-artist concentration = %v, want 1.0", r.ConcentrationWith)
	}
	if h := r.ConcentrationWithout; h < 0.33 || h > 0.34 {
		t.Errorf("three-artist concentration = %v, want ~0.333", h)
	}
}
