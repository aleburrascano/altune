package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
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

func TestTailNoiseInTopK(t *testing.T) {
	clean := deezerTrack("Clean", "Artist", 50)
	noise := track("type beat", "uploader", domain.ProviderSoundCloud, nil)
	results := []domain.SearchResult{clean, noise, noise, clean}

	if got := TailNoiseInTopK(results, 5); got != 2 {
		t.Errorf("top-5 noise = %d, want 2", got)
	}
	if got := TailNoiseInTopK(results, 1); got != 0 {
		t.Errorf("top-1 noise = %d, want 0 (clean result first)", got)
	}
	if got := TailNoiseInTopK(nil, 5); got != 0 {
		t.Errorf("empty = %d, want 0", got)
	}
}

func dzArtist(name string, pop float64) domain.SearchResult {
	return withPop(res(domain.ResultKindArtist, name, "", domain.ProviderDeezer, nil), pop)
}

func norm(raw string) string { return textnorm.NormalizeForMatch(raw) }

func indexOfTitle(results []domain.SearchResult, title string) int {
	for i, r := range results {
		if r.Title == title {
			return i
		}
	}
	return -1
}

func countTitle(results []domain.SearchResult, title string) int {
	n := 0
	for _, r := range results {
		if r.Title == title {
			n++
		}
	}
	return n
}

func inTopN(t *testing.T, got []domain.SearchResult, n int, wantTitle string) {
	t.Helper()
	limit := n
	if len(got) < limit {
		limit = len(got)
	}
	for _, r := range got[:limit] {
		if r.Title == wantTitle {
			return
		}
	}
	t.Fatalf("want %q in top-%d, got order %v", wantTitle, n, titles(got))
}

func TestRankPipeline_GateDominatesPopularity(t *testing.T) {
	itunesOnlyAlbum := withPop(res(domain.ResultKindAlbum, "Mirage", "Some Artist", domain.ProviderITunes, nil), 1_000_000)
	deezerTrack := deezerTrack("Mirage", "Some Artist", 10)

	got := rankPipeline([][]domain.SearchResult{{itunesOnlyAlbum}, {deezerTrack}}, norm("Mirage"))

	if len(got) == 0 {
		t.Fatalf("expected the browseable track to survive, got empty")
	}
	for _, r := range got {
		if r.Kind == domain.ResultKindAlbum {
			t.Fatalf("non-browseable album leaked into results despite the gate: %v", titles(got))
		}
	}
}

func TestRankPipeline_NoKindFavoritism(t *testing.T) {
	albumFirst := withPop(res(domain.ResultKindAlbum, "Echo", "Band", domain.ProviderDeezer, nil), 50)
	trackSecond := withPop(res(domain.ResultKindTrack, "Echo", "Band", domain.ProviderDeezer, nil), 50)

	got := rankPipeline([][]domain.SearchResult{{albumFirst, trackSecond}}, norm("Echo"))

	albumPos, trackPos := -1, -1
	for i, r := range got {
		switch r.Kind {
		case domain.ResultKindAlbum:
			albumPos = i
		case domain.ResultKindTrack:
			trackPos = i
		}
	}
	if albumPos == -1 || trackPos == -1 {
		t.Fatalf("expected both album and track to survive, got %v", titles(got))
	}
	if albumPos > trackPos {
		t.Fatalf("kind favoritism: track was promoted above the album that preceded it in input (album=%d track=%d)", albumPos, trackPos)
	}
}

func TestRankPipeline_DiversityPreservesOrder(t *testing.T) {
	in := []domain.SearchResult{
		deezerTrack("Song A", "Solo", 90),
		deezerTrack("Song B", "Solo", 80),
		deezerTrack("Song C", "Solo", 70),
		deezerTrack("Song D", "Solo", 60),
		deezerTrack("Song E", "Solo", 50),
	}
	got := rankPipeline([][]domain.SearchResult{in}, norm("Solo"))

	order := []string{"Song A", "Song B", "Song C", "Song D", "Song E"}
	prev := -1
	for _, title := range order {
		idx := indexOfTitle(got, title)
		if idx == -1 {
			t.Fatalf("%q dropped by the pipeline: %v", title, titles(got))
		}
		if idx <= prev {
			t.Fatalf("diversity promoted %q above its popularity rank: %v", title, titles(got))
		}
		prev = idx
	}
}

func TestRankPipeline_CollapseKeepsSurvivors(t *testing.T) {
	auroraLow := dzArtist("Aurora", 40)
	auroraHigh := dzArtist("Aurora", 95)
	auroraBorealis := dzArtist("Aurora Borealis", 30)

	got := rankPipeline(
		[][]domain.SearchResult{{auroraLow}, {auroraHigh}, {auroraBorealis}},
		norm("Aurora"),
	)

	if c := countTitle(got, "Aurora"); c != 1 {
		t.Fatalf("expected the two 'Aurora' artists to collapse to 1, got %d: %v", c, titles(got))
	}
	if indexOfTitle(got, "Aurora Borealis") == -1 {
		t.Fatalf("distinct artist 'Aurora Borealis' was wrongly collapsed: %v", titles(got))
	}
	survivor := got[indexOfTitle(got, "Aurora")]
	if survivor.Popularity != 95 {
		t.Fatalf("collapse kept the wrong (less popular) survivor: pop=%v", survivor.Popularity)
	}
}

func TestRankPipeline_CanonicalTopThree(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		groups    [][]domain.SearchResult
		wantTitle string
	}{
		{
			name:  "humble surfaces the kendrick track despite a more popular same-name artist",
			query: "Humble",
			groups: [][]domain.SearchResult{
				{dzArtist("Humble", 90)},
				{deezerTrack("HUMBLE.", "Kendrick Lamar", 40)},
				{deezerAlbum("Humble", "Some Artist", 30)},
			},
			wantTitle: "HUMBLE.",
		},
		{
			name:  "drake the artist surfaces among many drake tracks",
			query: "Drake",
			groups: [][]domain.SearchResult{
				{dzArtist("Drake", 100)},
				{
					deezerTrack("God's Plan", "Drake", 999),
					deezerTrack("Hotline Bling", "Drake", 950),
					deezerTrack("In My Feelings", "Drake", 900),
					deezerTrack("One Dance", "Drake", 880),
				},
			},
			wantTitle: "Drake",
		},
		{
			name:  "kendrick lamar humble lands the specific track",
			query: "Kendrick Lamar Humble",
			groups: [][]domain.SearchResult{
				{dzArtist("Humble", 90)},
				{deezerTrack("HUMBLE.", "Kendrick Lamar", 40)},
				{deezerTrack("DNA.", "Kendrick Lamar", 55)},
			},
			wantTitle: "HUMBLE.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rankPipeline(tt.groups, norm(tt.query))
			inTopN(t, got, 3, tt.wantTitle)
		})
	}
}

func topPick(r domain.SearchResult) domain.SearchResult {
	r.Extras = map[string]any{domain.ExtraTopPick: true}
	return r
}

func TestRank_ProvidersTopPickOutranksASameTitleSongByAnotherArtist(t *testing.T) {
	song := deezerTrack("Cochise", "Audioslave", 90)
	rapper := multiSource(res(domain.ResultKindArtist, "Cochise", "", domain.ProviderSpotify, nil), domain.ProviderDeezer)

	base := Rank([]Entity{ent(rapper), ent(song)}, "cochise")
	if len(base) != 2 || base[0].Kind != domain.ResultKindTrack {
		t.Fatalf("baseline precondition: expected the Audioslave song first, got %v", titles(base))
	}

	got := Rank([]Entity{ent(song), ent(topPick(rapper))}, "cochise")
	if len(got) != 2 || got[0].Kind != domain.ResultKindArtist {
		t.Fatalf("ranking = %v, want Spotify's top pick (the artist) first", titles(got))
	}
}

func TestRank_TopPickStillHasToShareAQueryWord(t *testing.T) {
	unrelated := topPick(res(domain.ResultKindArtist, "Someone Else", "", domain.ProviderSpotify, nil))
	match := deezerTrack("Humble", "Kendrick Lamar", 10)

	got := Rank([]Entity{ent(unrelated), ent(match)}, "humble")
	if len(got) != 1 || got[0].Title != "Humble" {
		t.Fatalf("ranking = %v, want only the result that shares a query word", titles(got))
	}
}

func TestRankWith_DemotedTopPickStaysBelowCleanResults(t *testing.T) {
	junk := topPick(track("rest in bass encore type beat", "prodguy", domain.ProviderSoundCloud, nil))
	clean := deezerTrack("Encore", "Che", 10)

	got := rankWith([]Entity{ent(junk), ent(clean)}, "rest in bass encore", rankConfig{demote: isLowConfidenceTail})
	if len(got) != 2 || got[0].Subtitle != "Che" {
		t.Fatalf("ranking = %v, want the clean result above the demoted top pick", titles(got))
	}
}

func pickedArtist(name string) domain.SearchResult {
	return topPick(multiSource(res(domain.ResultKindArtist, name, "", domain.ProviderSpotify, nil), domain.ProviderDeezer))
}

func TestRank_ThePickedArtistsSongsOutrankSameTitleSongsByOthers(t *testing.T) {
	other := deezerTrack("Cochise", "Audioslave", 90)
	own := deezerTrack("Hatchback", "Cochise", 10)

	got := Rank([]Entity{ent(other), ent(own), ent(pickedArtist("Cochise"))}, "cochise")

	want := []string{"Cochise", "Hatchback", "Cochise"}
	if len(got) != 3 || got[1].Subtitle != "Cochise" || got[2].Subtitle != "Audioslave" {
		t.Fatalf("ranking = %v by %v, want %v (pick, its song, then Audioslave's)", titles(got), subtitles(got), want)
	}
}

func TestRank_ThePickedArtistsSongsOutrankLookalikeArtists(t *testing.T) {
	lookalike := multiSource(res(domain.ResultKindArtist, "Drake Bell", "", domain.ProviderDeezer, nil), domain.ProviderSpotify)
	lookalikeSong := deezerTrack("Found a Way", "Drake Bell", 80)
	own := deezerTrack("One Dance", "Drake", 10)

	got := Rank([]Entity{ent(lookalike), ent(lookalikeSong), ent(own), ent(pickedArtist("Drake"))}, "drake")

	if len(got) != 4 || got[0].Title != "Drake" || got[1].Title != "One Dance" {
		t.Fatalf("ranking = %v, want Drake then One Dance above Drake Bell", titles(got))
	}
}

func TestCreditsArtist(t *testing.T) {
	tests := []struct {
		name, artist, credit string
		want                 bool
	}{
		{"main artist", "Drake", "Drake", true},
		{"different case", "Drake", "DRAKE", true},
		{"listed with a comma", "Drake", "Future, Drake", true},
		{"listed with an ampersand", "Drake", "Future & Drake", true},
		{"featured", "Drake", "Future feat. Drake", true},
		{"a name with a comma in it", "Tyler, The Creator", "Tyler, The Creator", true},
		{"a longer name that starts the same", "Drake", "Drake Bell", false},
		{"someone else", "Drake", "Audioslave", false},
		{"no credit", "Drake", "", false},
		{"a name made of symbols", "¥$", "¥$", true},
		{"a symbol name listed with others", "¥$", "¥$, Kanye West", true},
		{"another symbol name", "¥$", "$$$", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			song := deezerTrack("Some Song", tt.credit, 10)
			if got := creditsArtist(song, matchName(tt.artist)); got != tt.want {
				t.Errorf("creditsArtist(%q, %q) = %v, want %v", tt.credit, tt.artist, got, tt.want)
			}
		})
	}
}

func TestRank_ATrackPickDoesNotLiftItsArtistsOtherSongs(t *testing.T) {
	pick := topPick(deezerTrack("Humble", "Kendrick Lamar", 10))
	exact := deezerTrack("Humble", "Someone", 50)
	sameArtist := deezerTrack("Humble Beginnings", "Kendrick Lamar", 99)

	got := Rank([]Entity{ent(sameArtist), ent(exact), ent(pick)}, "humble")

	if len(got) != 3 || got[0].Subtitle != "Kendrick Lamar" || got[1].Subtitle != "Someone" {
		t.Fatalf("ranking = %v by %v, want the pick, then the exact title, then the partial", titles(got), subtitles(got))
	}
}
