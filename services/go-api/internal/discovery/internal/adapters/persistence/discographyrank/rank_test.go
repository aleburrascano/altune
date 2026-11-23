package discographyrank_test

import (
	"altune/go-api/internal/discovery/internal/adapters/persistence/discographyrank"
	"altune/go-api/internal/discovery/internal/core/ports"
	"slices"
	"testing"
	"time"
)

func dc(ref string, releases, single int, pc ...any) ports.DiscographyCase {
	counts := map[string]int{}
	for i := 0; i+1 < len(pc); i += 2 {
		counts[pc[i].(string)] = pc[i+1].(int)
	}
	return ports.DiscographyCase{ArtistRef: ref, Releases: releases, SingleProvider: single, ProviderCounts: counts}
}

func dcNoID(ref string, releases, single, noID int, pc ...any) ports.DiscographyCase {
	c := dc(ref, releases, single, pc...)
	c.SingleProviderNoID = noID
	return c
}

func refs(cases []ports.DiscographyCase) []string {
	out := make([]string, len(cases))
	for i, c := range cases {
		out[i] = c.ArtistRef
	}
	return out
}

func TestRankByArtist_WorstFirst(t *testing.T) {
	cases := []ports.DiscographyCase{
		dc("clean", 10, 0, "spotify", 10, "musicbrainz", 10),
		dc("worst", 10, 9, "spotify", 10, "musicbrainz", 1),
		dc("mid", 10, 5, "spotify", 8, "musicbrainz", 4),
		dc("tie-lo-imb", 10, 9, "spotify", 6, "musicbrainz", 5),
	}
	got := refs(discographyrank.Rank(cases, ports.GroupByArtist))
	want := []string{"worst", "tie-lo-imb", "mid", "clean"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestRankByArtist_IDAnchorBeatsHeadcount(t *testing.T) {
	cases := []ports.DiscographyCase{
		dcNoID("id-verified", 10, 10, 0, "spotify", 10),
		dcNoID("no-id", 10, 3, 3, "spotify", 7, "deezer", 3),
	}
	got := refs(discographyrank.Rank(cases, ports.GroupByArtist))
	want := []string{"no-id", "id-verified"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v (id anchor ranks the no-id artist first; headcount is only the fallback)", got, want)
	}
	if got[len(got)-1] != "id-verified" {
		t.Fatalf("id-verified single-provider artist ranked %v, want last (not a top suspect)", got)
	}
}

func TestRankByArtist_DivideByZeroSafe(t *testing.T) {
	cases := []ports.DiscographyCase{
		dc("zero-releases", 0, 5),
		dc("negative", -3, -9),
		dc("real", 4, 3, "spotify", 4, "deezer", 1),
	}
	got := refs(discographyrank.Rank(cases, ports.GroupByArtist))
	if got[0] != "real" {
		t.Fatalf("worst-first = %v, want real first (zero/neg ratios are 0)", got)
	}
}

func TestRankByProvider_ClustersWorstFirst(t *testing.T) {
	cases := []ports.DiscographyCase{
		dc("d1", 10, 1, "deezer", 8, "spotify", 3),
		dc("s1", 10, 9, "spotify", 10, "deezer", 1),
		dc("s2", 10, 7, "spotify", 9, "deezer", 2),
	}
	got := refs(discographyrank.Rank(cases, ports.GroupByProvider))
	want := []string{"s1", "s2", "d1"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestRankByContaminationBand(t *testing.T) {
	cases := []ports.DiscographyCase{
		dc("low", 10, 1),
		dc("high-a", 10, 6, "s", 6),
		dc("medium", 10, 3),
		dc("high-b", 10, 9, "s", 9),
	}
	got := refs(discographyrank.Rank(cases, ports.GroupByContaminationBand))
	want := []string{"high-b", "high-a", "medium", "low"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestRank_UnknownGroupByIsArtist(t *testing.T) {
	cases := []ports.DiscographyCase{
		dc("a", 10, 2),
		dc("b", 10, 8),
	}
	got := refs(discographyrank.Rank(cases, ports.DiscographyGroupBy("bogus")))
	if want := []string{"b", "a"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v (artist default)", got, want)
	}
}

func assertTiedOnScore(t *testing.T, x, y ports.DiscographyCase) {
	t.Helper()
	for _, labels := range [][2]string{{"a", "b"}, {"b", "a"}} {
		x.ArtistRef, y.ArtistRef = labels[0], labels[1]
		got := refs(discographyrank.Rank([]ports.DiscographyCase{y, x}, ports.GroupByArtist))
		if want := []string{"a", "b"}; !slices.Equal(got, want) {
			t.Errorf("with %v labelled %q and %v labelled %q, order = %v, want %v: they must tie and fall back to artist_ref",
				x.ProviderCounts, labels[0], y.ProviderCounts, labels[1], got, want)
		}
	}
}

func TestRankByArtist_NoCrownedSource(t *testing.T) {
	assertTiedOnScore(t,
		dcNoID("", 4, 4, 4, "musicbrainz", 4),
		dcNoID("", 4, 4, 4, "genius", 4),
	)
}

func TestRankByArtist_NoStaticTrustWeights(t *testing.T) {
	assertTiedOnScore(t,
		dcNoID("", 10, 5, 5, "spotify", 100, "deezer", 1),
		dcNoID("", 10, 5, 5, "musicbrainz", 2, "genius", 101),
	)
}

func TestRankByArtist_TieBreaksOnProviderImbalance(t *testing.T) {
	tests := []struct {
		name          string
		worse, better ports.DiscographyCase
	}{
		{"a lone provider has no imbalance", dc("b-spread", 5, 1, "p", 0, "q", 1), dc("a-lone", 5, 1, "only", 5)},
		{"negative counts are floored at zero", dc("b-wider", 5, 1, "a", 0, "b", 5), dc("a-negative", 5, 1, "a", -4, "b", 3)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := refs(discographyrank.Rank([]ports.DiscographyCase{tt.better, tt.worse}, ports.GroupByArtist))
			if want := []string{tt.worse.ArtistRef, tt.better.ArtistRef}; !slices.Equal(got, want) {
				t.Errorf("order = %v, want %v", got, want)
			}
		})
	}
}

func assertOrder(t *testing.T, cases []ports.DiscographyCase, groupBy ports.DiscographyGroupBy, want ...string) {
	t.Helper()
	if got := refs(discographyrank.Rank(cases, groupBy)); !slices.Equal(got, want) {
		t.Errorf("Rank(%s) order = %v, want %v", groupBy, got, want)
	}
}

func TestRank_IdenticalCasesKeepTheirInputOrder(t *testing.T) {
	first := time.Unix(1_700_000_000, 0)
	for _, groupBy := range []ports.DiscographyGroupBy{ports.GroupByArtist, ports.GroupByContaminationBand, ports.GroupByProvider} {
		t.Run(string(groupBy), func(t *testing.T) {
			a, b := dc("same", 10, 4, "spotify", 4), dc("same", 10, 4, "spotify", 4)
			a.LastSeen, b.LastSeen = first, first.Add(time.Hour)
			got := discographyrank.Rank([]ports.DiscographyCase{a, b}, groupBy)
			if len(got) != 2 || !got[0].LastSeen.Equal(first) {
				t.Errorf("Rank reordered two identical cases: %+v", got)
			}
		})
	}
}

func TestRankByContaminationBand_ThresholdsAreInclusive(t *testing.T) {
	assertOrder(t, []ports.DiscographyCase{
		dcNoID("d-tenth", 10, 1, 1),
		dcNoID("c-fifth", 10, 2, 0),
		dcNoID("b-two-fifths", 10, 4, 4),
		dcNoID("a-half", 10, 5, 0),
	}, ports.GroupByContaminationBand, "a-half", "b-two-fifths", "c-fifth", "d-tenth")
}

func TestRankByProvider_ClusterTiesBreakOnReleasesThenProvider(t *testing.T) {
	t.Run("more releases first", func(t *testing.T) {
		assertOrder(t, []ports.DiscographyCase{
			dc("s", 10, 5, "spotify", 5),
			dc("d", 20, 10, "deezer", 9),
		}, ports.GroupByProvider, "d", "s")
	})
	t.Run("then provider name", func(t *testing.T) {
		assertOrder(t, []ports.DiscographyCase{
			dc("b", 10, 5, "bprov", 5),
			dc("a", 10, 5, "aprov", 5),
		}, ports.GroupByProvider, "a", "b")
	})
	t.Run("ratio before releases", func(t *testing.T) {
		assertOrder(t, []ports.DiscographyCase{
			dc("p", 20, 2, "pp", 2),
			dc("q", 5, 2, "qq", 2),
		}, ports.GroupByProvider, "q", "p")
	})
}

func TestRankByProvider_AMemberWithoutReleasesAddsNothingToItsCluster(t *testing.T) {
	assertOrder(t, []ports.DiscographyCase{
		dc("p-zero", 0, 5, "pp", 1),
		dc("p-real", 10, 1, "pp", 1),
		dc("q", 5, 2, "qq", 1),
	}, ports.GroupByProvider, "q", "p-real", "p-zero")
}

func TestRankByProvider_ATieForTheDominantProviderGoesToTheFirstName(t *testing.T) {
	assertOrder(t, []ports.DiscographyCase{
		dc("x", 10, 1, "zeta", 5, "alpha", 5),
		dc("y", 10, 9, "alpha", 9),
		dc("z", 10, 6, "zeta", 9),
	}, ports.GroupByProvider, "z", "y", "x")
}

func TestRankByProvider_AZeroCountStillNamesItsProvider(t *testing.T) {
	assertOrder(t, []ports.DiscographyCase{
		dc("a", 10, 1),
		dc("b", 10, 5, "x", 0),
		dc("c", 10, 3, "m", 3),
	}, ports.GroupByProvider, "b", "c", "a")
}

func TestRankByArtist_ACaseWithoutReleasesCountsAsClean(t *testing.T) {
	assertOrder(t, []ports.DiscographyCase{
		dcNoID("no-releases", 0, 3, 3),
		dc("some-contamination", 10, 1),
	}, ports.GroupByArtist, "some-contamination", "no-releases")
}
