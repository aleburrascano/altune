package service

import (
	"altune/go-api/internal/discovery/domain"
	"math/rand"
	"testing"
)

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
	exact := deezerTrack("Humble", "Artist A", 10)
	partial := deezerTrack("Humble Beginnings", "Artist B", 99)

	got := Rank([]Entity{ent(partial), ent(exact)}, "humble")

	if len(got) == 0 || got[0].Title != "Humble" {
		t.Fatalf("ranking = %v, want exact 'Humble' first", titles(got))
	}
}

func TestRank_DashVariantOutranksParenForRemasterQuery(t *testing.T) {
	dash := deezerTrack("Big Poppa - 2005 Remaster", "The Notorious B.I.G.", 50)
	paren := deezerTrack("Big Poppa (2007 Remaster)", "The Notorious B.I.G.", 99)

	got := Rank([]Entity{ent(paren), ent(dash)}, "big poppa 2005 remaster")

	if len(got) == 0 || got[0].Title != "Big Poppa - 2005 Remaster" {
		t.Fatalf("ranking = %v, want the dash-form variant first", titles(got))
	}
}

func TestRank_PopularityBreaksRelevanceTie(t *testing.T) {
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

func TestRank_SingleCharQueryIsNotGatedOut(t *testing.T) {
	artist := deezerTrack("U", "U", 50)

	got := Rank([]Entity{ent(artist)}, "u")

	if len(got) != 1 {
		t.Fatalf("single-char query gated out every result, got %v", titles(got))
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

func withISRC(r domain.SearchResult, isrc string) domain.SearchResult {
	r.ISRC = isrc
	return r
}

func withAlbum(r domain.SearchResult, album string) domain.SearchResult {
	r.Album = album
	return r
}

func multiSource(r domain.SearchResult, p domain.ProviderName) domain.SearchResult {
	r.Sources = append(r.Sources, domain.SourceRef{Provider: p, ExternalID: "x", URL: "https://x"})
	return r
}

func TestIsLowConfidenceTail(t *testing.T) {
	tests := []struct {
		name string
		r    domain.SearchResult
		want bool
	}{
		{
			name: "single-source soundcloud, no identity → demoted",
			r:    track("che rest in bass encore type beat", "prodguy", domain.ProviderSoundCloud, nil),
			want: true,
		},
		{
			name: "single-source lastfm, no identity → demoted",
			r:    track("Intro", "che rest in bass", domain.ProviderLastFM, nil),
			want: true,
		},
		{
			name: "single-source soundcloud but carries album → rescued",
			r:    withAlbum(track("Real Song", "Artist", domain.ProviderSoundCloud, nil), "Real Album"),
			want: false,
		},
		{
			name: "single-source soundcloud but carries isrc → rescued",
			r:    withISRC(track("Real Song", "Artist", domain.ProviderSoundCloud, nil), "US1234567890"),
			want: false,
		},
		{
			name: "single-source deezer (curated catalog) → never demoted",
			r:    track("Real Song", "Artist", domain.ProviderDeezer, nil),
			want: false,
		},
		{
			name: "single-source itunes (curated catalog) → never demoted",
			r:    track("Real Song", "Artist", domain.ProviderITunes, nil),
			want: false,
		},
		{
			name: "multi-source soundcloud+lastfm (corroborated) → never demoted",
			r:    multiSource(track("BA$$", "che", domain.ProviderSoundCloud, nil), domain.ProviderLastFM),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLowConfidenceTail(tt.r); got != tt.want {
				t.Errorf("isLowConfidenceTail = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRankWith_DemotesUGCNoiseBelowCleanResult(t *testing.T) {
	junk := track("rest in bass encore type beat", "prodguy", domain.ProviderSoundCloud, nil)
	clean := deezerTrack("Encore", "Che", 10)

	base := Rank([]Entity{ent(clean), ent(junk)}, "rest in bass encore")
	if len(base) != 2 || base[0].Subtitle != "prodguy" {
		t.Fatalf("baseline precondition: expected junk first on relevance, got %v", titles(base))
	}

	got := rankWith([]Entity{ent(clean), ent(junk)}, "rest in bass encore", rankConfig{demote: isLowConfidenceTail})
	if len(got) != 2 || got[0].Subtitle != "Che" {
		t.Fatalf("with demotion: expected clean 'Che' first, got %v", titles(got))
	}
	if got[1].Subtitle != "prodguy" {
		t.Fatalf("with demotion: expected UGC noise last, got %v", titles(got))
	}
}

func TestRankWith_NilPredicateMatchesRank(t *testing.T) {
	junk := track("crazy bootleg edit", "uploader", domain.ProviderSoundCloud, nil)
	clean := deezerTrack("Crazy", "Gnarls Barkley", 50)
	entities := []Entity{ent(clean), ent(junk)}

	a := Rank(entities, "crazy")
	b := rankWith(entities, "crazy", rankConfig{})
	if len(a) != len(b) {
		t.Fatalf("length mismatch: %v vs %v", titles(a), titles(b))
	}
	for i := range a {
		if a[i].Title != b[i].Title || a[i].Subtitle != b[i].Subtitle {
			t.Fatalf("ordering mismatch at %d: %v vs %v", i, titles(a), titles(b))
		}
	}
}

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
			if tt.aWins && rankLess(tt.b, tt.a) {
				t.Error("both orderings true — not a strict weak ordering")
			}
		})
	}
}

func TestRankWith_BehavioralScoresMapAppliedBySignature(t *testing.T) {
	satisfied := deezerTrack("Humble", "Kendrick Lamar", 10)
	skipped := deezerTrack("Humble", "Cover Band", 90)
	entities := []Entity{ent(skipped), ent(satisfied)}

	scores := map[string]float64{
		domain.ResultSignature(satisfied): 2.5,
		domain.ResultSignature(skipped):   -1.0,
	}
	got := rankWith(entities, "humble", rankConfig{behavioral: scores})
	if got[0].Subtitle != "Kendrick Lamar" {
		t.Fatalf("want the behaviorally satisfied result first, got %q by %q", got[0].Title, got[0].Subtitle)
	}

	plain := Rank(entities, "humble")
	if plain[0].Subtitle != "Cover Band" {
		t.Fatalf("inert path: want the popular result first, got %q", plain[0].Subtitle)
	}
}

func TestRankExplain_SameOrderAsRankWith(t *testing.T) {
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

func TestRankPipelineNoReshape_SkipsListShaping(t *testing.T) {
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

func twoSourceTrack(title, artist string, rank int64) domain.SearchResult {
	r := track(title, artist, domain.ProviderITunes, nil)
	r.ProviderRank = rank
	r.Sources = append(r.Sources, domain.SourceRef{
		Provider: domain.ProviderMusicBrainz, ExternalID: title + ":mb", URL: "https://x/" + title,
	})
	return r
}

func artistWithFans(name string, nbFan int64) domain.SearchResult {
	r := res(domain.ResultKindArtist, name, "", domain.ProviderDeezer, nil)
	r.FanCount = nbFan
	return r
}

func TestProminence_OffBuriesArtist_OnLiftsIt(t *testing.T) {
	artist := artistWithFans("Boston", 5_000_000)
	trk := twoSourceTrack("Boston", "Augustana", 50_000)
	entities := []Entity{ent(trk), ent(artist)}

	off := Rank(entities, "boston")
	if off[0].Kind != domain.ResultKindTrack {
		t.Fatalf("prominence OFF: want track buried-state first, got %s %q", off[0].Kind, off[0].Title)
	}

	on := rankWith(entities, "boston", rankConfig{prominence: true})
	if on[0].Kind != domain.ResultKindArtist {
		t.Fatalf("prominence ON: want artist first, got %s %q", on[0].Kind, on[0].Title)
	}
}

func TestProminence_ObscureArtistStaysBelowProminentTrack(t *testing.T) {
	artist := artistWithFans("FireWork", 30)
	trk := twoSourceTrack("Firework", "Katy Perry", 900_000)
	entities := []Entity{ent(artist), ent(trk)}

	on := rankWith(entities, "firework", rankConfig{prominence: true})
	if on[0].Kind != domain.ResultKindTrack {
		t.Fatalf("prominence ON: want prominent track first, got %s %q", on[0].Kind, on[0].Title)
	}
}

func TestProminence_SameKindEqualProminenceFallsThrough(t *testing.T) {
	hi := track("Echo", "Artist Hi", domain.ProviderDeezer, nil)
	hi.ProviderRank = 900_000
	lo := track("Echo", "Artist Lo", domain.ProviderDeezer, nil)
	lo.ProviderRank = 900_000
	entities := []Entity{ent(hi), ent(lo)}

	off := Rank(entities, "echo")
	on := rankWith(entities, "echo", rankConfig{prominence: true})

	if len(off) != len(on) {
		t.Fatalf("length changed: off %d on %d", len(off), len(on))
	}
	for i := range off {
		if off[i].Subtitle != on[i].Subtitle {
			t.Fatalf("same-kind order changed at %d: off %q on %q", i, off[i].Subtitle, on[i].Subtitle)
		}
	}
}

func TestRankLess_StrictWeakOrdering(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	kinds := []domain.ResultKind{domain.ResultKindArtist, domain.ResultKindAlbum, domain.ResultKindTrack}
	pool := []float64{0, 1, 5, 10}
	randScored := func() scored {
		return scored{
			result: domain.SearchResult{
				Kind:     kinds[rng.Intn(len(kinds))],
				Title:    string(rune('a' + rng.Intn(3))),
				Subtitle: string(rune('a' + rng.Intn(3))),
			},
			relevance:  pool[rng.Intn(len(pool))],
			behavioral: pool[rng.Intn(len(pool))],
			prominence: pool[rng.Intn(len(pool))],
			pop:        pool[rng.Intn(len(pool))],
			rrf:        pool[rng.Intn(len(pool))],
			multi:      rng.Intn(2) == 0,
			demoted:    rng.Intn(2) == 0,
		}
	}
	equiv := func(x, y scored) bool { return !rankLess(x, y) && !rankLess(y, x) }
	for i := 0; i < 5000; i++ {
		a, b, c := randScored(), randScored(), randScored()
		if rankLess(a, b) && rankLess(b, a) {
			t.Fatalf("asymmetry violated:\na=%+v\nb=%+v", a, b)
		}
		if rankLess(a, b) && rankLess(b, c) && !rankLess(a, c) {
			t.Fatalf("transitivity violated:\na=%+v\nb=%+v\nc=%+v", a, b, c)
		}
		if equiv(a, b) && equiv(b, c) && !equiv(a, c) {
			t.Fatalf("incomparability not transitive:\na=%+v\nb=%+v\nc=%+v", a, b, c)
		}
	}
}
