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
	if r.Extras == nil {
		r.Extras = map[string]any{}
	}
	r.Extras["popularity"] = pop
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

func TestRank_PatternA_TierBeatsPopularity(t *testing.T) {
	// Structured query → intent kind = track. The same-named album is MORE
	// popular, yet the exact track must still rank #1 (T1 > T2), with the album
	// immediately below. This is the structural Pattern-A fix.
	trackHumble := deezerTrack("HUMBLE.", "Kendrick Lamar", 70)
	albumHumble := deezerAlbum("Humble", "Kendrick Lamar", 99)

	intent := BuildIntent("kendrick lamar humble", "Kendrick Lamar", "Humble")
	got := Rank([]Entity{ent(albumHumble), ent(trackHumble)}, "kendrick lamar humble", intent)

	if len(got) != 2 {
		t.Fatalf("got %d results, want 2: %v", len(got), titles(got))
	}
	if got[0].Kind != domain.ResultKindTrack {
		t.Errorf("rank[0] = %s %q, want the track at T1", got[0].Kind, got[0].Title)
	}
	if got[1].Kind != domain.ResultKindAlbum {
		t.Errorf("rank[1] = %s %q, want the album at T2", got[1].Kind, got[1].Title)
	}
}

func TestRank_BareQuery_PopularityDecidesWithinTier(t *testing.T) {
	// No structured intent → no kind preference → exact-title track and album
	// are both T1; the more popular one wins on popularity.
	trackHumble := deezerTrack("HUMBLE.", "Kendrick Lamar", 90)
	albumHumble := deezerAlbum("Humble", "Kendrick Lamar", 40)

	intent := BuildIntent("humble", "", "")
	got := Rank([]Entity{ent(albumHumble), ent(trackHumble)}, "humble", intent)

	if len(got) != 2 || got[0].Kind != domain.ResultKindTrack {
		t.Fatalf("want track first by popularity, got %v", titles(got))
	}
}

func TestRank_TierOrderingExactPartialWeak(t *testing.T) {
	exact := deezerTrack("Humble", "Artist A", 10)              // T1
	partial := deezerTrack("Humble Beginnings", "Artist B", 99) // T3 (contains target)
	weak := deezerTrack("Vibes Only", "Humble Crew", 99)        // T4 (shares only "humble" via artist)

	intent := BuildIntent("humble", "", "")
	got := Rank([]Entity{ent(partial), ent(weak), ent(exact)}, "humble", intent)

	want := []string{"Humble", "Humble Beginnings", "Vibes Only"}
	for i, w := range want {
		if i >= len(got) || got[i].Title != w {
			t.Fatalf("ranking = %v, want %v", titles(got), want)
		}
	}
}

func TestRank_SharesQueryWordGate(t *testing.T) {
	relevant := deezerTrack("Humble", "Artist A", 50)
	noise := deezerTrack("Completely Different", "Other", 99)

	intent := BuildIntent("humble", "", "")
	got := Rank([]Entity{ent(noise), ent(relevant)}, "humble", intent)

	if len(got) != 1 || got[0].Title != "Humble" {
		t.Fatalf("expected only the relevant result, got %v", titles(got))
	}
}

func TestRank_BrowseableSourceGate(t *testing.T) {
	// Album with no Deezer source can't load detail content → dropped.
	itunesAlbum := withPop(res(domain.ResultKindAlbum, "Humble", "Artist A", domain.ProviderITunes, nil), 99)
	track := deezerTrack("Humble", "Artist A", 10)

	intent := BuildIntent("humble", "", "")
	got := Rank([]Entity{ent(itunesAlbum), ent(track)}, "humble", intent)

	if len(got) != 1 || got[0].Kind != domain.ResultKindTrack {
		t.Fatalf("expected the album dropped (no Deezer source), got %v", titles(got))
	}
}

func TestRank_MultiSourceTiebreakWithinTier(t *testing.T) {
	// Two exact-title T1 entities, equal popularity; the multi-source one wins.
	single := deezerTrack("Crazy", "Artist A", 50)

	multi := withPop(track("Crazy", "Artist B", domain.ProviderDeezer, nil), 50)
	multi.Sources = append(multi.Sources, domain.SourceRef{
		Provider: domain.ProviderITunes, ExternalID: "x", URL: "https://x",
	})
	multiEntity := Entity{
		Result:   multi,
		BestRank: map[domain.ProviderName]int{domain.ProviderDeezer: 0, domain.ProviderITunes: 0},
	}

	intent := BuildIntent("crazy", "", "")
	got := Rank([]Entity{ent(single), multiEntity}, "crazy", intent)

	if len(got) != 2 || got[0].Subtitle != "Artist B" {
		t.Fatalf("expected the multi-source result first, got %v", titles(got))
	}
}
