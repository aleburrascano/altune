package main

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"strings"
	"testing"

	discoveryEval "altune/go-api/internal/discovery/service/eval"
)

func TestRenderArtistIntent_ListsFailingQueries(t *testing.T) {
	report := discoveryEval.ArtistIntentReport{
		K: 3, Total: 2, Evaluated: 2,
		Results: []discoveryEval.ArtistIntentResult{
			{Artist: "Drake", Outcome: discoveryEval.ArtistIntentPass, Pass: true, ArtistPos: 0},
			{
				Artist: "Burial", Outcome: discoveryEval.ArtistIntentBelowK, ArtistPos: 5, FirstTrackPos: -1,
				Expected: `artist card "Burial" in top 3`,
				Ranked:   []discoveryEval.ResultSummary{{Kind: "album", Title: "Burial"}},
			},
		},
	}

	out := renderArtistIntent(report)

	if !strings.Contains(out, "## Failing queries — 1") {
		t.Errorf("output lacks the failing-queries header:\n%s", out)
	}
	if !strings.Contains(out, `- "Burial" [below_k] expected artist card "Burial" in top 3; ranked: album:Burial; artist at #6`) {
		t.Errorf("output does not name the failing query with expected and ranked:\n%s", out)
	}
	if strings.Contains(out, `- "Drake"`) {
		t.Errorf("output lists a passing query:\n%s", out)
	}
}

type queryMapSearcher map[string][]domain.SearchResult

func (m queryMapSearcher) Search(_ context.Context, q string) ([]domain.SearchResult, error) {
	if r, ok := m[q]; ok {
		return r, nil
	}
	return []domain.SearchResult{}, nil
}

func TestRenderArtistIntent_NamesWhatRankedForARealRun(t *testing.T) {
	searcher := queryMapSearcher{
		"Air": {
			{Kind: domain.ResultKindTrack, Title: "Air"},
			{Kind: domain.ResultKindAlbum, Title: "Air"},
			{Kind: domain.ResultKindTrack, Title: "Air Supply"},
			{Kind: domain.ResultKindArtist, Title: "Air"},
		},
		"Drake": {{Kind: domain.ResultKindArtist, Title: "Drake"}},
	}

	report := discoveryEval.RunArtistIntentEval(context.Background(), []string{"Air", "Drake", "Nobody"}, searcher, 1, 3, "", nil)
	out := renderArtistIntent(report)

	if !report.Results[1].Pass || report.Results[0].Pass || len(report.Results) != 3 {
		t.Fatalf("Results pass flags = %+v, want one per query with only Drake passing", report.Results)
	}
	for _, want := range []string{
		"## Failing queries — 2",
		`- "Air" [buried] expected artist card "Air" in top 3; ranked: track:Air | album:Air | track:Air Supply; artist at #4`,
		`- "Nobody" [no_results] expected artist card "Nobody" in top 3; ranked: (nothing ranked)`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if fails := report.Failures(); len(fails) != 2 || fails[0].Attrs["expected"] != `artist card "Air" in top 3` {
		t.Errorf("Failures = %+v, want attrs to carry expected", fails)
	}
}

func TestRenderSignalA_CountsTheFullSetAndListsOnlyTop(t *testing.T) {
	gaps := make([]discoveryEval.CoverageGap, 60)
	for i := range gaps {
		gaps[i] = discoveryEval.CoverageGap{QueryNorm: fmt.Sprintf("q%02d", i), Count: 1}
	}
	report := &discoveryEval.CoverageReportA{Abandoned: gaps}

	out := renderSignalA(report, 30, 50)

	if !strings.Contains(out, "## Abandoned — 60") {
		t.Errorf("headline count is not the full set of 60:\n%s", out)
	}
	if !strings.Contains(out, `"q49"`) || strings.Contains(out, `"q50"`) {
		t.Errorf("listing is not capped at top=50:\n%s", out)
	}
	if !strings.Contains(out, "10 more") {
		t.Errorf("output does not say 10 entries were not listed:\n%s", out)
	}
}
