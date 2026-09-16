package persistence

import (
	"altune/go-api/internal/discovery/ports"
	"testing"
)

// dc builds a DiscographyCase; providerCounts pairs are name,count,name,count...
func dc(ref string, releases, single int, pc ...any) ports.DiscographyCase {
	counts := map[string]int{}
	for i := 0; i+1 < len(pc); i += 2 {
		counts[pc[i].(string)] = pc[i+1].(int)
	}
	return ports.DiscographyCase{ArtistRef: ref, Releases: releases, SingleProvider: single, ProviderCounts: counts}
}

func refs(cases []ports.DiscographyCase) []string {
	out := make([]string, len(cases))
	for i, c := range cases {
		out[i] = c.ArtistRef
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRankByArtist_WorstFirst is the core seam guarantee: the artist with the
// highest single-provider ratio ranks first, imbalance breaks a ratio tie.
func TestRankByArtist_WorstFirst(t *testing.T) {
	cases := []ports.DiscographyCase{
		dc("clean", 10, 0, "spotify", 10, "musicbrainz", 10),    // ratio 0.0
		dc("worst", 10, 9, "spotify", 10, "musicbrainz", 1),     // ratio 0.9
		dc("mid", 10, 5, "spotify", 8, "musicbrainz", 4),        // ratio 0.5
		dc("tie-lo-imb", 10, 9, "spotify", 6, "musicbrainz", 5), // ratio 0.9, imbalance 1
	}
	got := refs(rankDiscographyCases(cases, ports.GroupByArtist))
	want := []string{"worst", "tie-lo-imb", "mid", "clean"}
	if !eq(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestRankByArtist_DivideByZeroSafe proves a zero-release (or negative) case never
// panics and never sorts as worst: its ratio is 0.
func TestRankByArtist_DivideByZeroSafe(t *testing.T) {
	cases := []ports.DiscographyCase{
		dc("zero-releases", 0, 5),
		dc("negative", -3, -9),
		dc("real", 4, 3, "spotify", 4, "deezer", 1),
	}
	got := refs(rankDiscographyCases(cases, ports.GroupByArtist))
	if got[0] != "real" {
		t.Fatalf("worst-first = %v, want real first (zero/neg ratios are 0)", got)
	}
}

// TestRankByProvider_ClustersWorstFirst proves by=provider clusters artists by
// their dominant provider and orders the clusters worst-first by aggregate ratio.
func TestRankByProvider_ClustersWorstFirst(t *testing.T) {
	cases := []ports.DiscographyCase{
		// deezer-dominant cluster, low contamination
		dc("d1", 10, 1, "deezer", 8, "spotify", 3),
		// spotify-dominant cluster, high contamination
		dc("s1", 10, 9, "spotify", 10, "deezer", 1),
		dc("s2", 10, 7, "spotify", 9, "deezer", 2),
	}
	got := refs(rankDiscographyCases(cases, ports.GroupByProvider))
	// spotify cluster (agg ratio 0.8) before deezer cluster (0.1); within spotify
	// s1 (0.9) before s2 (0.7).
	want := []string{"s1", "s2", "d1"}
	if !eq(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestRankByContaminationBand orders high band before medium before low, worst
// artist first within a band.
func TestRankByContaminationBand(t *testing.T) {
	cases := []ports.DiscographyCase{
		dc("low", 10, 1),            // 0.1 -> low
		dc("high-a", 10, 6, "s", 6), // 0.6 -> high
		dc("medium", 10, 3),         // 0.3 -> medium
		dc("high-b", 10, 9, "s", 9), // 0.9 -> high
	}
	got := refs(rankDiscographyCases(cases, ports.GroupByContaminationBand))
	want := []string{"high-b", "high-a", "medium", "low"}
	if !eq(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestRank_UnknownGroupByIsArtist proves an unknown grouping degrades to the
// artist ordering rather than dropping or reordering unpredictably.
func TestRank_UnknownGroupByIsArtist(t *testing.T) {
	cases := []ports.DiscographyCase{
		dc("a", 10, 2),
		dc("b", 10, 8),
	}
	got := refs(rankDiscographyCases(cases, ports.DiscographyGroupBy("bogus")))
	if want := []string{"b", "a"}; !eq(got, want) {
		t.Fatalf("order = %v, want %v (artist default)", got, want)
	}
}

// TestProviderImbalance_AdversarialCounts proves negative provider counts are
// floored so imbalance stays non-negative and a lone provider has no imbalance.
func TestProviderImbalance_AdversarialCounts(t *testing.T) {
	if got := providerImbalance(dc("x", 5, 1, "only", 5)); got != 0 {
		t.Errorf("lone provider imbalance = %d, want 0", got)
	}
	if got := providerImbalance(dc("x", 5, 1, "a", -4, "b", 3)); got != 3 {
		t.Errorf("adversarial imbalance = %d, want 3 (negatives floored)", got)
	}
}
