package eval

import (
	"context"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// fakeVariantSearcher returns canned (withReshape, withoutReshape) lists per query.
type fakeVariantSearcher struct {
	with    map[string][]domain.SearchResult
	without map[string][]domain.SearchResult
}

func (f *fakeVariantSearcher) SearchVariants(_ context.Context, q string) ([]domain.SearchResult, []domain.SearchResult) {
	return f.with[q], f.without[q]
}

func TestRunDiversityEval_costWhenReshapeDemotesTarget(t *testing.T) {
	entity := LibraryEntity{Title: "Humble", Artist: "Kendrick"}
	q := "Kendrick Humble"

	// WITHOUT reshape: target is at position 1 (in top-2). WITH reshape: the
	// per-artist cap pushed it to position 3 (out of top-2) — a cost.
	other := track("Other", "Kendrick", domain.ProviderDeezer, nil)
	target := track("Humble", "Kendrick", domain.ProviderDeezer, nil)
	fake := &fakeVariantSearcher{
		without: map[string][]domain.SearchResult{q: {target, other}},
		with:    map[string][]domain.SearchResult{q: {other, other, target}},
	}
	r := RunDiversityEval(context.Background(), []LibraryEntity{entity}, fake, 1, 2, nil)

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

func TestRunDiversityEval_noCostWhenTargetSurvives(t *testing.T) {
	entity := LibraryEntity{Title: "Humble", Artist: "Kendrick"}
	q := "Kendrick Humble"
	target := track("Humble", "Kendrick", domain.ProviderDeezer, nil)
	fake := &fakeVariantSearcher{
		without: map[string][]domain.SearchResult{q: {target}},
		with:    map[string][]domain.SearchResult{q: {target}},
	}
	r := RunDiversityEval(context.Background(), []LibraryEntity{entity}, fake, 1, 3, nil)
	if r.LostToReshape != 0 || r.CostRate() != 0 {
		t.Errorf("target survived reshape but cost reported: lost=%d rate=%v", r.LostToReshape, r.CostRate())
	}
}

func TestTopKConcentration(t *testing.T) {
	// Three results, all the same artist → Herfindahl 1.0 (max concentration).
	same := []domain.SearchResult{
		track("A", "Drake", domain.ProviderDeezer, nil),
		track("B", "Drake", domain.ProviderDeezer, nil),
		track("C", "Drake", domain.ProviderDeezer, nil),
	}
	if h := topKConcentration(same, 3); h != 1.0 {
		t.Errorf("all-one-artist concentration = %v, want 1.0", h)
	}
	// Three distinct artists → 3 × (1/3)^2 = 1/3.
	diverse := []domain.SearchResult{
		track("A", "Drake", domain.ProviderDeezer, nil),
		track("B", "Adele", domain.ProviderDeezer, nil),
		track("C", "Queen", domain.ProviderDeezer, nil),
	}
	if h := topKConcentration(diverse, 3); h < 0.33 || h > 0.34 {
		t.Errorf("three-artist concentration = %v, want ~0.333", h)
	}
}
