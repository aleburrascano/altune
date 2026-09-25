package eval

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service"
	"context"
	"errors"
	"reflect"
	"testing"
)

func stubProvider(name string, titles ...string) service.ConsensusProvider {
	return service.ConsensusProvider{
		Name: name,
		Fetcher: func(_ context.Context, _ string) ([]domain.SearchResult, error) {
			out := make([]domain.SearchResult, 0, len(titles))
			for _, t := range titles {
				out = append(out, domain.SearchResult{Kind: domain.ResultKindAlbum, Title: t})
			}
			return out, nil
		},
	}
}

func gapFor(report *CoverageReportB, provider string) ProviderGap {
	for _, g := range report.ProviderGaps {
		if g.Provider == provider {
			return g
		}
	}
	return ProviderGap{}
}

func TestCoverageSignalB_AttributesGapsToMissingProvider(t *testing.T) {
	providers := []service.ConsensusProvider{
		stubProvider("p1", "Beta", "Gamma", "Delta"),
		stubProvider("p2", "Alpha", "Gamma", "Delta"),
		stubProvider("p3", "Alpha", "Beta", "Delta"),
		stubProvider("p4", "Alpha", "Beta", "Gamma"),
	}
	svc := NewCoverageSignalBService(providers)

	report, err := svc.Execute(context.Background(), []string{"X"}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.TotalEntities != 4 {
		t.Fatalf("TotalEntities = %d, want 4", report.TotalEntities)
	}
	if report.ArtistsScanned != 1 {
		t.Errorf("ArtistsScanned = %d, want 1", report.ArtistsScanned)
	}
	for _, name := range []string{"p1", "p2", "p3", "p4"} {
		g := gapFor(report, name)
		if g.Missing != 1 || g.Union != 4 || g.GapPct != 0.25 {
			t.Errorf("%s gap = %+v, want missing 1 / union 4 / 0.25", name, g)
		}
	}
}

func TestCoverageSignalB_AllProvidersHaveItIsZeroGap(t *testing.T) {
	providers := []service.ConsensusProvider{
		stubProvider("p1", "Solo Album"),
		stubProvider("p2", "Solo Album"),
		stubProvider("p3", "Solo Album"),
	}
	svc := NewCoverageSignalBService(providers)

	report, err := svc.Execute(context.Background(), []string{"X"}, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.TotalEntities != 1 {
		t.Errorf("TotalEntities = %d, want 1", report.TotalEntities)
	}
	for _, g := range report.ProviderGaps {
		if g.GapPct != 0 {
			t.Errorf("%s gap = %.2f, want 0 (all-providers-miss is invisible here)", g.Provider, g.GapPct)
		}
	}
	if len(report.Caveats) == 0 {
		t.Error("expected caveats to be stated in the report")
	}
}

func TestCoverageSignalB_EntityLevelNotCount(t *testing.T) {
	providers := []service.ConsensusProvider{
		stubProvider("p1", "DAMN."),
		stubProvider("p2", "Damn"),
	}
	svc := NewCoverageSignalBService(providers)

	report, err := svc.Execute(context.Background(), []string{"Kendrick Lamar"}, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.TotalEntities != 1 {
		t.Fatalf("TotalEntities = %d, want 1 (titles must merge to one entity)", report.TotalEntities)
	}
	if g := gapFor(report, "p1"); g.GapPct != 0 {
		t.Errorf("p1 gap = %.2f, want 0 (no false gap from title variance)", g.GapPct)
	}
	if g := gapFor(report, "p2"); g.GapPct != 0 {
		t.Errorf("p2 gap = %.2f, want 0 (no false gap from title variance)", g.GapPct)
	}
}

// artistStubProvider returns per-artist titles; an artist absent from the map fails.
func artistStubProvider(name string, byArtist map[string][]string) service.ConsensusProvider {
	return service.ConsensusProvider{
		Name: name,
		Fetcher: func(_ context.Context, artist string) ([]domain.SearchResult, error) {
			titles, ok := byArtist[artist]
			if !ok {
				return nil, errors.New("stub failure")
			}
			out := make([]domain.SearchResult, 0, len(titles))
			for _, t := range titles {
				out = append(out, domain.SearchResult{Kind: domain.ResultKindAlbum, Title: t})
			}
			return out, nil
		},
	}
}

// Pins the exact per-provider counters (missing, union, unique) across
// several artists, partial provider responses and a fully-failed artist.
func TestCoverageSignalB_PinsPerProviderTallies(t *testing.T) {
	providers := []service.ConsensusProvider{
		artistStubProvider("p1", map[string][]string{
			"A": {"Alpha", "Beta", "Only One"},
			"B": {"Red"},
		}),
		artistStubProvider("p2", map[string][]string{
			"A": {"Alpha", "Gamma"},
			"B": {"Red", "Blue", "Green"},
		}),
		artistStubProvider("p3", map[string][]string{
			"A": {"Alpha", "Beta", "Gamma", "Three Only"},
		}),
		artistStubProvider("p4", map[string][]string{}),
	}
	svc := NewCoverageSignalBService(providers)

	report, err := svc.Execute(context.Background(), []string{"A", "B", "Nobody"}, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Artist A: entities Alpha{p1,p2,p3} Beta{p1,p3} OnlyOne{p1} Gamma{p2,p3} ThreeOnly{p3}
	//   responded p1,p2,p3 -> union 5 each; missing p1=2 p2=3 p3=1; unique p1=1 p3=1.
	// Artist B: entities Red{p1,p2} Blue{p2} Green{p2}
	//   responded p1,p2 -> union 3 each; missing p1=2 p2=0; unique p2=2.
	// Artist Nobody: no provider responded -> skipped.
	if report.ArtistsScanned != 2 {
		t.Errorf("ArtistsScanned = %d, want 2", report.ArtistsScanned)
	}
	if report.TotalEntities != 8 {
		t.Errorf("TotalEntities = %d, want 8", report.TotalEntities)
	}
	want := []ProviderGap{
		{Provider: "p1", Missing: 4, Union: 8, GapPct: 0.5, Unique: 1},
		{Provider: "p2", Missing: 3, Union: 8, GapPct: 0.375, Unique: 2},
		{Provider: "p3", Missing: 1, Union: 5, GapPct: 0.2, Unique: 1},
		{Provider: "p4", Missing: 0, Union: 0, GapPct: 0, Unique: 0},
	}
	if !reflect.DeepEqual(report.ProviderGaps, want) {
		t.Errorf("ProviderGaps = %+v\nwant %+v", report.ProviderGaps, want)
	}
}
