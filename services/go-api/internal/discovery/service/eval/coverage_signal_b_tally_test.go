package eval

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service"
	"context"
	"errors"
	"reflect"
	"testing"
)

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
