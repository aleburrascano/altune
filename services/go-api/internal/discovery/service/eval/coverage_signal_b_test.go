package eval

import (
	"context"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service"
)

// stubProvider returns a fixed album list regardless of artist.
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
	// 4 entities, each missing from exactly one provider → 25% gap each.
	providers := []service.ConsensusProvider{
		stubProvider("p1", "Beta", "Gamma", "Delta"),  // missing Alpha
		stubProvider("p2", "Alpha", "Gamma", "Delta"), // missing Beta
		stubProvider("p3", "Alpha", "Beta", "Delta"),  // missing Gamma
		stubProvider("p4", "Alpha", "Beta", "Gamma"),  // missing Delta
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
	// Same album under slightly different titles must resolve to one entity.
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
