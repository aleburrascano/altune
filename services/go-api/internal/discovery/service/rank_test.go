package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// ent wraps a result as a single-provider entity at rank 0.
func ent(r domain.SearchResult) Entity {
	br := make(map[domain.ProviderName]int)
	for _, s := range r.Sources {
		br[s.Provider] = 0
	}
	return Entity{Result: r, BestRank: br}
}

func withPop(r domain.SearchResult, pop float64) domain.SearchResult {
	r.Popularity = pop
	return r
}

// deezerTrack / deezerAlbum carry a Deezer source so they pass the browseable gate.
func deezerTrack(title, artist string, pop float64) domain.SearchResult {
	return withPop(track(title, artist, domain.ProviderDeezer, nil), pop)
}

func deezerAlbum(title, artist string, pop float64) domain.SearchResult {
	return withPop(res(domain.ResultKindAlbum, title, artist, domain.ProviderDeezer, nil), pop)
}

func titles(results []domain.SearchResult) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Title
	}
	return out
}

func TestRank_ExactTitleOutranksPartial(t *testing.T) {
	// Continuous relevance: the exact title matches more of the query than a
	// partial one, so it ranks higher — even when the partial is more popular.
	exact := deezerTrack("Humble", "Artist A", 10)
	partial := deezerTrack("Humble Beginnings", "Artist B", 99)

	got := Rank([]Entity{ent(partial), ent(exact)}, "humble")

	if len(got) == 0 || got[0].Title != "Humble" {
		t.Fatalf("ranking = %v, want exact 'Humble' first", titles(got))
	}
}

func TestRank_DashVariantOutranksParenForRemasterQuery(t *testing.T) {
	// The regression fix, in the ranker: for a query carrying the version suffix,
	// the dash-form variant (which keeps those tokens after normalization) is a
	// closer match than the paren-form (whose suffix normalization strips away).
	dash := deezerTrack("Big Poppa - 2005 Remaster", "The Notorious B.I.G.", 50)
	paren := deezerTrack("Big Poppa (2007 Remaster)", "The Notorious B.I.G.", 99)

	got := Rank([]Entity{ent(paren), ent(dash)}, "big poppa 2005 remaster")

	if len(got) == 0 || got[0].Title != "Big Poppa - 2005 Remaster" {
		t.Fatalf("ranking = %v, want the dash-form variant first", titles(got))
	}
}

func TestRank_PopularityBreaksRelevanceTie(t *testing.T) {
	// Same canonical title → equal relevance → popularity decides.
	a := deezerTrack("Crazy", "Artist A", 30)
	b := deezerTrack("Crazy", "Artist B", 90)

	got := Rank([]Entity{ent(a), ent(b)}, "crazy")

	if len(got) != 2 || got[0].Subtitle != "Artist B" {
		t.Fatalf("ranking = %v, want the more popular 'Crazy' first", titles(got))
	}
}

func TestRank_SharesQueryWordGate(t *testing.T) {
	relevant := deezerTrack("Humble", "Artist A", 50)
	noise := deezerTrack("Completely Different", "Other", 99)

	got := Rank([]Entity{ent(noise), ent(relevant)}, "humble")

	if len(got) != 1 || got[0].Title != "Humble" {
		t.Fatalf("expected only the relevant result, got %v", titles(got))
	}
}

func TestRank_BrowseableSourceGate(t *testing.T) {
	itunesAlbum := withPop(res(domain.ResultKindAlbum, "Humble", "Artist A", domain.ProviderITunes, nil), 99)
	trk := deezerTrack("Humble", "Artist A", 10)

	got := Rank([]Entity{ent(itunesAlbum), ent(trk)}, "humble")

	if len(got) != 1 || got[0].Kind != domain.ResultKindTrack {
		t.Fatalf("expected the album dropped (no Deezer source), got %v", titles(got))
	}
}

func TestRank_MultiSourceTiebreakWithinEqualRelevanceAndPopularity(t *testing.T) {
	single := deezerTrack("Crazy", "Artist A", 50)

	multi := withPop(track("Crazy", "Artist A", domain.ProviderDeezer, nil), 50)
	multi.Sources = append(multi.Sources, domain.SourceRef{
		Provider: domain.ProviderITunes, ExternalID: "x", URL: "https://x",
	})
	multiEntity := Entity{
		Result:   multi,
		BestRank: map[domain.ProviderName]int{domain.ProviderDeezer: 0, domain.ProviderITunes: 0},
	}

	got := Rank([]Entity{ent(single), multiEntity}, "crazy")

	if len(got) != 2 || len(got[0].Sources) != 2 {
		t.Fatalf("expected the multi-source result first, got %v", titles(got))
	}
}
