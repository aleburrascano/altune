package rank_test

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/search/service/rank"
	"altune/go-api/internal/shared/textnorm"
	"testing"
)

func ent(r domain.SearchResult) rank.Entity {
	br := make(map[domain.ProviderName]int)
	for _, s := range r.Sources {
		br[s.Provider] = 0
	}
	return rank.Entity{Result: r, BestRank: br}
}

func TestRank_ExactTitleOutranksPartial(t *testing.T) {
	exact := searchtest.DeezerTrack("Humble", "Artist A", 10)
	partial := searchtest.DeezerTrack("Humble Beginnings", "Artist B", 99)

	got := rank.Rank([]rank.Entity{ent(partial), ent(exact)}, "humble")

	if len(got) == 0 || got[0].Title != "Humble" {
		t.Fatalf("ranking = %v, want exact 'Humble' first", searchtest.Titles(got))
	}
}

func TestRank_DashVariantOutranksParenForRemasterQuery(t *testing.T) {
	dash := searchtest.DeezerTrack("Big Poppa - 2005 Remaster", "The Notorious B.I.G.", 50)
	paren := searchtest.DeezerTrack("Big Poppa (2007 Remaster)", "The Notorious B.I.G.", 99)

	got := rank.Rank([]rank.Entity{ent(paren), ent(dash)}, "big poppa 2005 remaster")

	if len(got) == 0 || got[0].Title != "Big Poppa - 2005 Remaster" {
		t.Fatalf("ranking = %v, want the dash-form variant first", searchtest.Titles(got))
	}
}

func TestRank_PopularityBreaksRelevanceTie(t *testing.T) {
	a := searchtest.DeezerTrack("Crazy", "Artist A", 30)
	b := searchtest.DeezerTrack("Crazy", "Artist B", 90)

	got := rank.Rank([]rank.Entity{ent(a), ent(b)}, "crazy")

	if len(got) != 2 || got[0].Subtitle != "Artist B" {
		t.Fatalf("ranking = %v, want the more popular 'Crazy' first", searchtest.Titles(got))
	}
}

func TestRank_SharesQueryWordGate(t *testing.T) {
	relevant := searchtest.DeezerTrack("Humble", "Artist A", 50)
	noise := searchtest.DeezerTrack("Completely Different", "Other", 99)

	got := rank.Rank([]rank.Entity{ent(noise), ent(relevant)}, "humble")

	if len(got) != 1 || got[0].Title != "Humble" {
		t.Fatalf("expected only the relevant result, got %v", searchtest.Titles(got))
	}
}

func TestRank_SingleCharQueryIsNotGatedOut(t *testing.T) {
	artist := searchtest.DeezerTrack("U", "U", 50)

	got := rank.Rank([]rank.Entity{ent(artist)}, "u")

	if len(got) != 1 {
		t.Fatalf("single-char query gated out every result, got %v", searchtest.Titles(got))
	}
}

func TestRank_BrowseableSourceGate(t *testing.T) {
	itunesAlbum := searchtest.WithPop(searchtest.Res(domain.ResultKindAlbum, "Humble", "Artist A", domain.ProviderITunes, nil), 99)
	trk := searchtest.DeezerTrack("Humble", "Artist A", 10)

	got := rank.Rank([]rank.Entity{ent(itunesAlbum), ent(trk)}, "humble")

	if len(got) != 1 || got[0].Kind != domain.ResultKindTrack {
		t.Fatalf("expected the album dropped (no Deezer source), got %v", searchtest.Titles(got))
	}
}

func TestRank_MultiSourceTiebreakWithinEqualRelevanceAndPopularity(t *testing.T) {
	single := searchtest.DeezerTrack("Crazy", "Artist A", 50)

	multi := searchtest.WithPop(searchtest.Track("Crazy", "Artist A", domain.ProviderDeezer, nil), 50)
	multi.Sources = append(multi.Sources, domain.ProviderLink{
		Provider: domain.ProviderITunes, ExternalID: "x", URL: "https://x",
	})
	multiEntity := rank.Entity{
		Result:   multi,
		BestRank: map[domain.ProviderName]int{domain.ProviderDeezer: 0, domain.ProviderITunes: 0},
	}

	got := rank.Rank([]rank.Entity{ent(single), multiEntity}, "crazy")

	if len(got) != 2 || len(got[0].Sources) != 2 {
		t.Fatalf("expected the multi-source result first, got %v", searchtest.Titles(got))
	}
}

func withAlbum(r domain.SearchResult, album string) domain.SearchResult {
	r.Album = album
	return r
}

func multiSource(r domain.SearchResult, p domain.ProviderName) domain.SearchResult {
	r.Sources = append(r.Sources, domain.ProviderLink{Provider: p, ExternalID: "x", URL: "https://x"})
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
			r:    searchtest.Track("che rest in bass encore type beat", "prodguy", domain.ProviderSoundCloud, nil),
			want: true,
		},
		{
			name: "single-source spotify, no identity → never demoted",
			r:    searchtest.Track("Intro", "che rest in bass", domain.ProviderSpotify, nil),
			want: false,
		},
		{
			name: "single-source soundcloud but carries album → rescued",
			r:    withAlbum(searchtest.Track("Real Song", "Artist", domain.ProviderSoundCloud, nil), "Real Album"),
			want: false,
		},
		{
			name: "single-source soundcloud but carries isrc → rescued",
			r:    searchtest.WithISRC(searchtest.Track("Real Song", "Artist", domain.ProviderSoundCloud, nil), "US1234567890"),
			want: false,
		},
		{
			name: "single-source deezer (curated catalog) → never demoted",
			r:    searchtest.Track("Real Song", "Artist", domain.ProviderDeezer, nil),
			want: false,
		},
		{
			name: "single-source itunes (curated catalog) → never demoted",
			r:    searchtest.Track("Real Song", "Artist", domain.ProviderITunes, nil),
			want: false,
		},
		{
			name: "multi-source soundcloud+deezer (corroborated) → never demoted",
			r:    multiSource(searchtest.Track("BA$$", "che", domain.ProviderSoundCloud, nil), domain.ProviderDeezer),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rank.TailNoiseInTopK([]domain.SearchResult{tt.r}, 1) == 1; got != tt.want {
				t.Errorf("counted as tail noise = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRankWith_DemotesUGCNoiseBelowCleanResult(t *testing.T) {
	junk := searchtest.Track("rest in bass encore type beat", "prodguy", domain.ProviderSoundCloud, nil)
	clean := searchtest.DeezerTrack("Encore", "Che", 10)

	base := rank.Rank([]rank.Entity{ent(clean), ent(junk)}, "rest in bass encore")
	if len(base) != 2 || base[0].Subtitle != "prodguy" {
		t.Fatalf("baseline precondition: expected junk first on relevance, got %v", searchtest.Titles(base))
	}

	got := rank.RankWith([]rank.Entity{ent(clean), ent(junk)}, "rest in bass encore", rank.RankOptions{TailDemotion: true})
	if len(got) != 2 || got[0].Subtitle != "Che" {
		t.Fatalf("with demotion: expected clean 'Che' first, got %v", searchtest.Titles(got))
	}
	if got[1].Subtitle != "prodguy" {
		t.Fatalf("with demotion: expected UGC noise last, got %v", searchtest.Titles(got))
	}
}

func TestRankWith_NilPredicateMatchesRank(t *testing.T) {
	junk := searchtest.Track("crazy bootleg edit", "uploader", domain.ProviderSoundCloud, nil)
	clean := searchtest.DeezerTrack("Crazy", "Gnarls Barkley", 50)
	entities := []rank.Entity{ent(clean), ent(junk)}

	a := rank.Rank(entities, "crazy")
	b := rank.RankWith(entities, "crazy", rank.RankOptions{})
	if len(a) != len(b) {
		t.Fatalf("length mismatch: %v vs %v", searchtest.Titles(a), searchtest.Titles(b))
	}
	for i := range a {
		if a[i].Title != b[i].Title || a[i].Subtitle != b[i].Subtitle {
			t.Fatalf("ordering mismatch at %d: %v vs %v", i, searchtest.Titles(a), searchtest.Titles(b))
		}
	}
}

func TestRankWith_BehavioralScoresMapAppliedBySignature(t *testing.T) {
	satisfied := searchtest.DeezerTrack("Humble", "Kendrick Lamar", 10)
	skipped := searchtest.DeezerTrack("Humble", "Cover Band", 90)
	entities := []rank.Entity{ent(skipped), ent(satisfied)}

	scores := map[string]float64{
		domain.ResultSignature(satisfied): 2.5,
		domain.ResultSignature(skipped):   -1.0,
	}
	got := rank.RankWith(entities, "humble", rank.RankOptions{Behavioral: scores})
	if got[0].Subtitle != "Kendrick Lamar" {
		t.Fatalf("want the behaviorally satisfied result first, got %q by %q", got[0].Title, got[0].Subtitle)
	}

	plain := rank.Rank(entities, "humble")
	if plain[0].Subtitle != "Cover Band" {
		t.Fatalf("inert path: want the popular result first, got %q", plain[0].Subtitle)
	}
}

func TestRankPipelineNoReshape_SkipsListShaping(t *testing.T) {
	group := []domain.SearchResult{}
	for _, title := range []string{"Humble A", "Humble B", "Humble C", "Humble D", "Humble E", "Humble F"} {
		group = append(group, searchtest.DeezerTrack(title, "Kendrick Lamar", 50))
	}
	perProvider := [][]domain.SearchResult{group}

	without := rank.RankPipelineNoReshape(perProvider, "humble")
	if len(without) != 6 {
		t.Fatalf("no-reshape results = %d, want all 6", len(without))
	}
	with := rank.RankPipeline(perProvider, "humble")
	if len(with) > len(without) {
		t.Fatalf("reshaped %d > unshaped %d — reshape must never invent results", len(with), len(without))
	}
}

func twoSourceTrack(title, artist string, rank int64) domain.SearchResult {
	r := searchtest.Track(title, artist, domain.ProviderITunes, nil)
	r.ProviderRank = rank
	r.Sources = append(r.Sources, domain.ProviderLink{
		Provider: domain.ProviderMusicBrainz, ExternalID: title + ":mb", URL: "https://x/" + title,
	})
	return r
}

func artistWithFans(name string, nbFan int64) domain.SearchResult {
	r := searchtest.Res(domain.ResultKindArtist, name, "", domain.ProviderDeezer, nil)
	r.FanCount = nbFan
	return r
}

func TestProminence_OffBuriesArtist_OnLiftsIt(t *testing.T) {
	artist := artistWithFans("Boston", 5_000_000)
	trk := twoSourceTrack("Boston", "Augustana", 50_000)
	entities := []rank.Entity{ent(trk), ent(artist)}

	off := rank.Rank(entities, "boston")
	if off[0].Kind != domain.ResultKindTrack {
		t.Fatalf("prominence OFF: want track buried-state first, got %s %q", off[0].Kind, off[0].Title)
	}

	on := rank.RankWith(entities, "boston", rank.RankOptions{CrossKindProminence: true})
	if on[0].Kind != domain.ResultKindArtist {
		t.Fatalf("prominence ON: want artist first, got %s %q", on[0].Kind, on[0].Title)
	}
}

func TestProminence_ObscureArtistStaysBelowProminentTrack(t *testing.T) {
	artist := artistWithFans("FireWork", 30)
	trk := twoSourceTrack("Firework", "Katy Perry", 900_000)
	entities := []rank.Entity{ent(artist), ent(trk)}

	on := rank.RankWith(entities, "firework", rank.RankOptions{CrossKindProminence: true})
	if on[0].Kind != domain.ResultKindTrack {
		t.Fatalf("prominence ON: want prominent track first, got %s %q", on[0].Kind, on[0].Title)
	}
}

func TestProminence_SameKindEqualProminenceFallsThrough(t *testing.T) {
	hi := searchtest.Track("Echo", "Artist Hi", domain.ProviderDeezer, nil)
	hi.ProviderRank = 900_000
	lo := searchtest.Track("Echo", "Artist Lo", domain.ProviderDeezer, nil)
	lo.ProviderRank = 900_000
	entities := []rank.Entity{ent(hi), ent(lo)}

	off := rank.Rank(entities, "echo")
	on := rank.RankWith(entities, "echo", rank.RankOptions{CrossKindProminence: true})

	if len(off) != len(on) {
		t.Fatalf("length changed: off %d on %d", len(off), len(on))
	}
	for i := range off {
		if off[i].Subtitle != on[i].Subtitle {
			t.Fatalf("same-kind order changed at %d: off %q on %q", i, off[i].Subtitle, on[i].Subtitle)
		}
	}
}

func TestTailNoiseInTopK(t *testing.T) {
	clean := searchtest.DeezerTrack("Clean", "Artist", 50)
	noise := searchtest.Track("type beat", "uploader", domain.ProviderSoundCloud, nil)
	results := []domain.SearchResult{clean, noise, noise, clean}

	if got := rank.TailNoiseInTopK(results, 5); got != 2 {
		t.Errorf("top-5 noise = %d, want 2", got)
	}
	if got := rank.TailNoiseInTopK(results, 1); got != 0 {
		t.Errorf("top-1 noise = %d, want 0 (clean result first)", got)
	}
	if got := rank.TailNoiseInTopK(nil, 5); got != 0 {
		t.Errorf("empty = %d, want 0", got)
	}
}

func dzArtist(name string, pop float64) domain.SearchResult {
	return searchtest.WithPop(searchtest.Res(domain.ResultKindArtist, name, "", domain.ProviderDeezer, nil), pop)
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
	t.Fatalf("want %q in top-%d, got order %v", wantTitle, n, searchtest.Titles(got))
}

func TestRankPipeline_GateDominatesPopularity(t *testing.T) {
	itunesOnlyAlbum := searchtest.WithPop(searchtest.Res(domain.ResultKindAlbum, "Mirage", "Some Artist", domain.ProviderITunes, nil), 1_000_000)
	deezerTrack := searchtest.DeezerTrack("Mirage", "Some Artist", 10)

	got := rank.RankPipeline([][]domain.SearchResult{{itunesOnlyAlbum}, {deezerTrack}}, norm("Mirage"))

	if len(got) == 0 {
		t.Fatalf("expected the browseable track to survive, got empty")
	}
	for _, r := range got {
		if r.Kind == domain.ResultKindAlbum {
			t.Fatalf("non-browseable album leaked into results despite the gate: %v", searchtest.Titles(got))
		}
	}
}

func TestRankPipeline_NoKindFavoritism(t *testing.T) {
	albumFirst := searchtest.WithPop(searchtest.Res(domain.ResultKindAlbum, "Echo", "Band", domain.ProviderDeezer, nil), 50)
	trackSecond := searchtest.WithPop(searchtest.Res(domain.ResultKindTrack, "Echo", "Band", domain.ProviderDeezer, nil), 50)

	got := rank.RankPipeline([][]domain.SearchResult{{albumFirst, trackSecond}}, norm("Echo"))

	albumPos, trackPos := -1, -1
	for i, r := range got {
		switch r.Kind {
		case domain.ResultKindAlbum:
			albumPos = i
		case domain.ResultKindTrack:
			trackPos = i
		case domain.ResultKindUnknown, domain.ResultKindArtist, domain.ResultKindPlaylist:
			t.Fatalf("unexpected result kind %v in the ranked output", r.Kind)
		}
	}
	if albumPos == -1 || trackPos == -1 {
		t.Fatalf("expected both album and track to survive, got %v", searchtest.Titles(got))
	}
	if albumPos > trackPos {
		t.Fatalf("kind favoritism: track was promoted above the album that preceded it in input (album=%d track=%d)", albumPos, trackPos)
	}
}

func TestRankPipeline_DiversityPreservesOrder(t *testing.T) {
	in := []domain.SearchResult{
		searchtest.DeezerTrack("Song A", "Solo", 90),
		searchtest.DeezerTrack("Song B", "Solo", 80),
		searchtest.DeezerTrack("Song C", "Solo", 70),
		searchtest.DeezerTrack("Song D", "Solo", 60),
		searchtest.DeezerTrack("Song E", "Solo", 50),
	}
	got := rank.RankPipeline([][]domain.SearchResult{in}, norm("Solo"))

	order := []string{"Song A", "Song B", "Song C", "Song D", "Song E"}
	prev := -1
	for _, title := range order {
		idx := indexOfTitle(got, title)
		if idx == -1 {
			t.Fatalf("%q dropped by the pipeline: %v", title, searchtest.Titles(got))
		}
		if idx <= prev {
			t.Fatalf("diversity promoted %q above its popularity rank: %v", title, searchtest.Titles(got))
		}
		prev = idx
	}
}

func TestRankPipeline_CollapseKeepsSurvivors(t *testing.T) {
	auroraLow := dzArtist("Aurora", 40)
	auroraHigh := dzArtist("Aurora", 95)
	auroraBorealis := dzArtist("Aurora Borealis", 30)

	got := rank.RankPipeline(
		[][]domain.SearchResult{{auroraLow}, {auroraHigh}, {auroraBorealis}},
		norm("Aurora"),
	)

	if c := countTitle(got, "Aurora"); c != 1 {
		t.Fatalf("expected the two 'Aurora' artists to collapse to 1, got %d: %v", c, searchtest.Titles(got))
	}
	if indexOfTitle(got, "Aurora Borealis") == -1 {
		t.Fatalf("distinct artist 'Aurora Borealis' was wrongly collapsed: %v", searchtest.Titles(got))
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
				{searchtest.DeezerTrack("HUMBLE.", "Kendrick Lamar", 40)},
				{searchtest.DeezerAlbum("Humble", "Some Artist", 30)},
			},
			wantTitle: "HUMBLE.",
		},
		{
			name:  "drake the artist surfaces among many drake tracks",
			query: "Drake",
			groups: [][]domain.SearchResult{
				{dzArtist("Drake", 100)},
				{
					searchtest.DeezerTrack("God's Plan", "Drake", 999),
					searchtest.DeezerTrack("Hotline Bling", "Drake", 950),
					searchtest.DeezerTrack("In My Feelings", "Drake", 900),
					searchtest.DeezerTrack("One Dance", "Drake", 880),
				},
			},
			wantTitle: "Drake",
		},
		{
			name:  "kendrick lamar humble lands the specific track",
			query: "Kendrick Lamar Humble",
			groups: [][]domain.SearchResult{
				{dzArtist("Humble", 90)},
				{searchtest.DeezerTrack("HUMBLE.", "Kendrick Lamar", 40)},
				{searchtest.DeezerTrack("DNA.", "Kendrick Lamar", 55)},
			},
			wantTitle: "HUMBLE.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rank.RankPipeline(tt.groups, norm(tt.query))
			inTopN(t, got, 3, tt.wantTitle)
		})
	}
}

func topPick(r domain.SearchResult) domain.SearchResult {
	r.Extras = map[string]any{domain.ExtraTopPick: true}
	return r
}

func TestRank_ProvidersTopPickOutranksASameTitleSongByAnotherArtist(t *testing.T) {
	song := searchtest.DeezerTrack("Cochise", "Audioslave", 90)
	rapper := multiSource(searchtest.Res(domain.ResultKindArtist, "Cochise", "", domain.ProviderSpotify, nil), domain.ProviderDeezer)

	base := rank.Rank([]rank.Entity{ent(rapper), ent(song)}, "cochise")
	if len(base) != 2 || base[0].Kind != domain.ResultKindTrack {
		t.Fatalf("baseline precondition: expected the Audioslave song first, got %v", searchtest.Titles(base))
	}

	got := rank.Rank([]rank.Entity{ent(song), ent(topPick(rapper))}, "cochise")
	if len(got) != 2 || got[0].Kind != domain.ResultKindArtist {
		t.Fatalf("ranking = %v, want Spotify's top pick (the artist) first", searchtest.Titles(got))
	}
}

func TestRank_TopPickStillHasToShareAQueryWord(t *testing.T) {
	unrelated := topPick(searchtest.Res(domain.ResultKindArtist, "Someone Else", "", domain.ProviderSpotify, nil))
	match := searchtest.DeezerTrack("Humble", "Kendrick Lamar", 10)

	got := rank.Rank([]rank.Entity{ent(unrelated), ent(match)}, "humble")
	if len(got) != 1 || got[0].Title != "Humble" {
		t.Fatalf("ranking = %v, want only the result that shares a query word", searchtest.Titles(got))
	}
}

func TestRankWith_DemotedTopPickStaysBelowCleanResults(t *testing.T) {
	junk := topPick(searchtest.Track("rest in bass encore type beat", "prodguy", domain.ProviderSoundCloud, nil))
	clean := searchtest.DeezerTrack("Encore", "Che", 10)

	got := rank.RankWith([]rank.Entity{ent(junk), ent(clean)}, "rest in bass encore", rank.RankOptions{TailDemotion: true})
	if len(got) != 2 || got[0].Subtitle != "Che" {
		t.Fatalf("ranking = %v, want the clean result above the demoted top pick", searchtest.Titles(got))
	}
}

func pickedArtist(name string) domain.SearchResult {
	return topPick(multiSource(searchtest.Res(domain.ResultKindArtist, name, "", domain.ProviderSpotify, nil), domain.ProviderDeezer))
}

func TestRank_ThePickedArtistsSongsOutrankSameTitleSongsByOthers(t *testing.T) {
	other := searchtest.DeezerTrack("Cochise", "Audioslave", 90)
	own := searchtest.DeezerTrack("Hatchback", "Cochise", 10)

	got := rank.Rank([]rank.Entity{ent(other), ent(own), ent(pickedArtist("Cochise"))}, "cochise")

	want := []string{"Cochise", "Hatchback", "Cochise"}
	if len(got) != 3 || got[1].Subtitle != "Cochise" || got[2].Subtitle != "Audioslave" {
		t.Fatalf("ranking = %v by %v, want %v (pick, its song, then Audioslave's)", searchtest.Titles(got), searchtest.Subtitles(got), want)
	}
}

func TestRank_ThePickedArtistsSongsOutrankLookalikeArtists(t *testing.T) {
	lookalike := multiSource(searchtest.Res(domain.ResultKindArtist, "Drake Bell", "", domain.ProviderDeezer, nil), domain.ProviderSpotify)
	lookalikeSong := searchtest.DeezerTrack("Found a Way", "Drake Bell", 80)
	own := searchtest.DeezerTrack("One Dance", "Drake", 10)

	got := rank.Rank([]rank.Entity{ent(lookalike), ent(lookalikeSong), ent(own), ent(pickedArtist("Drake"))}, "drake")

	if len(got) != 4 || got[0].Title != "Drake" || got[1].Title != "One Dance" {
		t.Fatalf("ranking = %v, want Drake then One Dance above Drake Bell", searchtest.Titles(got))
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
			song := searchtest.DeezerTrack("Some Song", tt.credit, 10)
			decoy := searchtest.DeezerTrack("Some Song", "Someone Else", 99)

			got := rank.Rank([]rank.Entity{ent(decoy), ent(song), ent(pickedArtist(tt.artist))}, "")

			if len(got) != 3 {
				t.Fatalf("ranking = %v by %v, want the pick and both songs", searchtest.Titles(got), searchtest.Subtitles(got))
			}
			if credited := got[1].Subtitle == tt.credit; credited != tt.want {
				t.Errorf("credit %q ranked as the picked artist %q's song = %v, want %v (ranking by %v)", tt.credit, tt.artist, credited, tt.want, searchtest.Subtitles(got))
			}
		})
	}
}

func TestRank_ATrackPickDoesNotLiftItsArtistsOtherSongs(t *testing.T) {
	pick := topPick(searchtest.DeezerTrack("Humble", "Kendrick Lamar", 10))
	exact := searchtest.DeezerTrack("Humble", "Someone", 50)
	sameArtist := searchtest.DeezerTrack("Humble Beginnings", "Kendrick Lamar", 99)

	got := rank.Rank([]rank.Entity{ent(sameArtist), ent(exact), ent(pick)}, "humble")

	if len(got) != 3 || got[0].Subtitle != "Kendrick Lamar" || got[1].Subtitle != "Someone" {
		t.Fatalf("ranking = %v by %v, want the pick, then the exact title, then the partial", searchtest.Titles(got), searchtest.Subtitles(got))
	}
}

func TestRank_CorroboratedResultOutranksABetterSingleProviderRank(t *testing.T) {
	single := searchtest.DeezerTrack("Echo", "Artist A", 10)
	corroborated := multiSource(searchtest.DeezerTrack("Echo", "Artist B", 10), domain.ProviderSpotify)
	entities := []rank.Entity{
		{Result: single, BestRank: map[domain.ProviderName]int{domain.ProviderDeezer: 0}},
		{Result: corroborated, BestRank: map[domain.ProviderName]int{domain.ProviderDeezer: 200, domain.ProviderSpotify: 200}},
	}

	got := rank.Rank(entities, "echo")

	if len(got) != 2 || got[0].Subtitle != "Artist B" {
		t.Fatalf("ranking = %v, want the two-provider Artist B first over the single provider's better rank", searchtest.Subtitles(got))
	}
}
