package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// --- demotion × prominence × behavioral interaction matrix ----------------

// The three experiment rungs sit at different rank tiers: demotion overrides
// relevance; prominence breaks relevance ties above behavioral; behavioral
// breaks what prominence left tied; popularity is last. Decision-table pins.
func TestRankLess_ExperimentTierMatrix(t *testing.T) {
	tests := []struct {
		name  string
		a, b  scored
		aWins bool
	}{
		{
			name:  "demotion overrides higher relevance",
			a:     scored{relevance: 0.2, demoted: false},
			b:     scored{relevance: 1.0, demoted: true},
			aWins: true,
		},
		{
			name:  "both demoted fall through to relevance",
			a:     scored{relevance: 1.0, demoted: true},
			b:     scored{relevance: 0.5, demoted: true},
			aWins: true,
		},
		{
			name:  "relevance dominates prominence",
			a:     scored{relevance: 1.0, prominence: 0},
			b:     scored{relevance: 0.5, prominence: 99},
			aWins: true,
		},
		{
			name:  "prominence breaks a relevance tie before behavioral",
			a:     scored{relevance: 1.0, prominence: 5, behavioral: -3},
			b:     scored{relevance: 1.0, prominence: 1, behavioral: 3},
			aWins: true,
		},
		{
			name:  "equal prominence falls through to behavioral",
			a:     scored{relevance: 1.0, prominence: 5, behavioral: 2},
			b:     scored{relevance: 1.0, prominence: 5, behavioral: -1},
			aWins: true,
		},
		{
			name:  "behavioral dominates popularity",
			a:     scored{relevance: 1.0, behavioral: 1, pop: 0},
			b:     scored{relevance: 1.0, behavioral: 0, pop: 99},
			aWins: true,
		},
		{
			name:  "demoted-but-prominent still loses to a plain result",
			a:     scored{relevance: 0.1},
			b:     scored{relevance: 1.0, prominence: 99, behavioral: 9, demoted: true},
			aWins: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rankLess(tt.a, tt.b); got != tt.aWins {
				t.Errorf("rankLess(a,b) = %v, want %v", got, tt.aWins)
			}
			// Strict weak ordering sanity: a wins ⇒ b must not also win.
			if tt.aWins && rankLess(tt.b, tt.a) {
				t.Error("both orderings true — not a strict weak ordering")
			}
		})
	}
}

func TestRankWith_BehavioralScoresMapAppliedBySignature(t *testing.T) {
	// Two same-relevance tracks; the behavioral map keys on ResultSignature and
	// must lift the satisfied one. The map is exactly what the Service snapshot
	// feeds rankPipelineWith.
	satisfied := deezerTrack("Humble", "Kendrick Lamar", 10)
	skipped := deezerTrack("Humble", "Cover Band", 90) // more popular — behavioral must outrank popularity
	entities := []Entity{ent(skipped), ent(satisfied)}

	scores := map[string]float64{
		domain.ResultSignature(satisfied): 2.5,
		domain.ResultSignature(skipped):   -1.0,
	}
	got := rankWith(entities, "humble", rankConfig{behavioral: scores})
	if got[0].Subtitle != "Kendrick Lamar" {
		t.Fatalf("want the behaviorally satisfied result first, got %q by %q", got[0].Title, got[0].Subtitle)
	}

	// Without the map, popularity decides — proving the map (not fixture order)
	// flipped the outcome above.
	plain := Rank(entities, "humble")
	if plain[0].Subtitle != "Cover Band" {
		t.Fatalf("inert path: want the popular result first, got %q", plain[0].Subtitle)
	}
}

// --- RankExplain parity ---------------------------------------------------

func TestRankExplain_SameOrderAsRankWith(t *testing.T) {
	// Property: for the same entities/query/options, RankExplain's result order
	// is IDENTICAL to RankWith's — the explainer must never diverge from what
	// production ranked. Exercised with every experiment rung active.
	trk := withISRC(deezerTrack("Boston", "Augustana", 40), "III")
	artist := artistWithFans("Boston", 4_000_000)
	tail := track("Boston Remix Boston", "reuploader", domain.ProviderSoundCloud, nil)
	other := deezerTrack("Boston Nights", "Someone", 70)
	entities := []Entity{ent(trk), ent(artist), ent(tail), ent(other)}

	opts := RankOptions{
		TailDemotion:        true,
		CrossKindProminence: true,
		Behavioral:          map[string]float64{domain.ResultSignature(other): 1.5},
	}
	ranked := RankWith(entities, "boston", opts)
	explained := RankExplain(entities, "boston", opts)

	if len(ranked) != len(explained) {
		t.Fatalf("lengths differ: ranked %d, explained %d", len(ranked), len(explained))
	}
	for i := range ranked {
		if ranked[i].Title != explained[i].Result.Title || ranked[i].Subtitle != explained[i].Result.Subtitle {
			t.Errorf("position %d: ranked %q/%q vs explained %q/%q",
				i, ranked[i].Title, ranked[i].Subtitle, explained[i].Result.Title, explained[i].Result.Subtitle)
		}
	}

	// Provenance sanity: the demoted UGC tail is flagged, the artist carries
	// prominence, the behavioral score is surfaced.
	byTitle := map[string]ScoredResult{}
	for _, s := range explained {
		byTitle[s.Result.Title+"|"+s.Result.Subtitle] = s
	}
	if !byTitle["Boston Remix Boston|reuploader"].Demoted {
		t.Error("UGC single-source result must be flagged Demoted in the explain output")
	}
	if byTitle["Boston|"].Prominence <= 0 {
		t.Error("prominent artist must carry a positive Prominence in the explain output")
	}
	if byTitle["Boston Nights|Someone"].Behavioral != 1.5 {
		t.Errorf("Behavioral = %v, want the supplied 1.5", byTitle["Boston Nights|Someone"].Behavioral)
	}
}

// --- rankPipelineNoReshape -------------------------------------------------

func TestRankPipelineNoReshape_SkipsListShaping(t *testing.T) {
	// Six same-artist tracks: the reshaped pipeline caps the artist in the top
	// window; the no-reshape baseline keeps all six in rank order. Their diff is
	// exactly what the diversity harness measures.
	group := []domain.SearchResult{}
	for _, title := range []string{"Humble A", "Humble B", "Humble C", "Humble D", "Humble E", "Humble F"} {
		group = append(group, deezerTrack(title, "Kendrick Lamar", 50))
	}
	perProvider := [][]domain.SearchResult{group}

	without := rankPipelineNoReshape(perProvider, "humble")
	if len(without) != 6 {
		t.Fatalf("no-reshape results = %d, want all 6", len(without))
	}
	with := rankPipeline(perProvider, "humble")
	if len(with) > len(without) {
		t.Fatalf("reshaped %d > unshaped %d — reshape must never invent results", len(with), len(without))
	}
}
