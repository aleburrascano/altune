package rank_test

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/search/service/rank"
	"testing"
)

const (
	mergeCapPerProvider    = 100
	cappedMergeAllocBudget = 10000
)

func distinctTracks(n int) []domain.SearchResult {
	tracks := make([]domain.SearchResult, n)
	for i := range tracks {
		tracks[i] = searchtest.Track("title "+string(rune(0x4e00+i)), "artist "+string(rune(0x4e00+i)), domain.ProviderDeezer, nil)
	}
	return tracks
}

func TestMerge_CapsEachProviderGroupBeforeTheQuadraticScan(t *testing.T) {
	huge := distinctTracks(10000)
	entities := rank.Merge([][]domain.SearchResult{huge})
	if len(entities) != mergeCapPerProvider {
		t.Fatalf("got %d entities, want %d", len(entities), mergeCapPerProvider)
	}
}

func TestMerge_CapsEachProviderGroupBeforeMatchingAcrossIt(t *testing.T) {
	huge := distinctTracks(10000)
	allocs := testing.AllocsPerRun(3, func() { rank.Merge([][]domain.SearchResult{huge}) })
	if allocs > cappedMergeAllocBudget {
		t.Fatalf("merging %d results allocated %.0f times, want at most %d: only the first %d of a group may be matched", len(huge), allocs, cappedMergeAllocBudget, mergeCapPerProvider)
	}
}

func findByTitle(t *testing.T, entities []rank.Entity, title string) rank.Entity {
	t.Helper()
	for _, e := range entities {
		if e.Result.Title == title {
			return e
		}
	}
	t.Fatalf("no entity titled %q in %d entities", title, len(entities))
	return rank.Entity{}
}

func TestMerge_UnambiguousArtistNameStillNameMerges(t *testing.T) {
	mb := searchtest.Res(domain.ResultKindArtist, "Drake", "", domain.ProviderDeezer, nil)
	itunes := searchtest.Res(domain.ResultKindArtist, "Drake", "", domain.ProviderITunes, nil)

	entities := rank.Merge([][]domain.SearchResult{{mb}, {itunes}})

	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1 (name merge preserved when unambiguous)", len(entities))
	}
	if got := len(entities[0].Result.Sources); got != 2 {
		t.Errorf("merged entity has %d sources, want 2", got)
	}
}

func TestMerge_IdentifierMatch(t *testing.T) {
	t.Run("same isrc merges across providers", func(t *testing.T) {
		a := searchtest.WithISRC(searchtest.Track("HUMBLE.", "Kendrick Lamar", domain.ProviderDeezer, nil), "USUM71703089")
		b := searchtest.WithISRC(searchtest.Track("Humble", "Kendrick Lamar", domain.ProviderITunes, nil), "USUM71703089")
		entities := rank.Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 1 {
			t.Fatalf("got %d entities, want 1", len(entities))
		}
		if got := len(entities[0].Result.Sources); got != 2 {
			t.Errorf("sources = %d, want 2 (unioned)", got)
		}
		if tier := entities[0].Result.ResolutionTier.Tier.String(); tier != "isrc" {
			t.Errorf("resolution_tier = %v, want isrc", tier)
		}
		if entities[0].Result.Confidence != domain.ConfidenceHigh {
			t.Errorf("confidence = %v, want high", entities[0].Result.Confidence)
		}
	})
}

func TestMerge_AlbumUPCTier(t *testing.T) {
	t.Run("same upc merges albums across providers", func(t *testing.T) {
		a := searchtest.Res(domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", domain.ProviderAppleMusic, nil)
		a.UPC = "00602557618280"
		b := searchtest.Res(domain.ResultKindAlbum, "DAMN. (Deluxe)", "Kendrick Lamar", domain.ProviderDeezer, nil)
		b.UPC = "00602557618280"
		entities := rank.Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 1 {
			t.Fatalf("got %d entities, want 1", len(entities))
		}
		if tier := entities[0].Result.ResolutionTier.Tier.String(); tier != "upc" {
			t.Errorf("resolution_tier = %v, want upc", tier)
		}
		if entities[0].Result.Confidence != domain.ConfidenceHigh {
			t.Errorf("confidence = %v, want high", entities[0].Result.Confidence)
		}
		if entities[0].Result.UPC != "00602557618280" {
			t.Errorf("merged UPC = %q, want coalesced", entities[0].Result.UPC)
		}
	})

	t.Run("mismatched upc does not block a canonical-title merge", func(t *testing.T) {
		a := searchtest.Res(domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", domain.ProviderAppleMusic, nil)
		a.UPC = "00602557618280"
		b := searchtest.Res(domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", domain.ProviderDeezer, nil)
		b.UPC = "00602557618297"
		entities := rank.Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 1 {
			t.Fatalf("got %d entities, want 1 (upc mismatch must not block)", len(entities))
		}
	})

	t.Run("upc never merges tracks", func(t *testing.T) {
		a := searchtest.Track("Same Barcode", "Artist A", domain.ProviderDeezer, nil)
		a.UPC = "00602557618280"
		b := searchtest.Track("Different Title", "Artist B", domain.ProviderITunes, nil)
		b.UPC = "00602557618280"
		entities := rank.Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 (upc tier is album-only)", len(entities))
		}
	})
}

func TestMergeInto_CoalescesTypedContentFields(t *testing.T) {
	a := searchtest.Track("HUMBLE.", "Kendrick Lamar", domain.ProviderDeezer, nil)
	a.Album = "DAMN."
	a.Duration = 177
	a.DeezerAlbumID = "13114014"
	b := searchtest.Track("HUMBLE.", "Kendrick Lamar", domain.ProviderITunes, nil)
	entities := rank.Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(entities))
	}
	r := entities[0].Result
	if r.Album != "DAMN." || r.Duration != 177 || r.DeezerAlbumID != "13114014" {
		t.Errorf("typed content fields dropped on merge: album=%q duration=%d deezerAlbumID=%q",
			r.Album, r.Duration, r.DeezerAlbumID)
	}
}

func TestMerge_SequelStaysSeparate(t *testing.T) {
	a := searchtest.Track("Shotta Flow", "NLE Choppa", domain.ProviderDeezer, map[string]any{"popularity": 90.0})
	b := searchtest.Track("Shotta Flow 2", "NLE Choppa", domain.ProviderDeezer, map[string]any{"popularity": 40.0})
	entities := rank.Merge([][]domain.SearchResult{{a, b}})
	if len(entities) != 2 {
		t.Fatalf("got %d entities, want 2 — the sequel must not collapse into the original", len(entities))
	}
}

func TestMerge_ParentheticalVariantsCollapse(t *testing.T) {
	cases := []struct{ a, b string }{
		{"Bad", "Bad (Remix)"},
		{"Fix You", "Fix You (Live)"},
		{"Goosebumps", "Goosebumps (feat. Kevin Abstract)"},
	}
	for _, tc := range cases {
		a := searchtest.Track(tc.a, "Some Artist", domain.ProviderDeezer, nil)
		b := searchtest.Track(tc.b, "Some Artist", domain.ProviderITunes, nil)
		entities := rank.Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 1 {
			t.Errorf("%q + %q: got %d entities, want 1 (parenthetical variant folds in)", tc.a, tc.b, len(entities))
		}
	}
}

func TestMerge_DashAndParenVariantsStaySeparate(t *testing.T) {
	a := searchtest.Track("Big Poppa - 2005 Remaster", "The Notorious B.I.G.", domain.ProviderTheAudioDB, nil)
	b := searchtest.Track("Big Poppa (2007 Remaster)", "The Notorious B.I.G.", domain.ProviderDeezer, nil)
	entities := rank.Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 2 {
		t.Fatalf("got %d entities, want 2 — the dash-form variant must stay distinct", len(entities))
	}
}

func TestMerge_SameTitleAcrossProvidersMerges(t *testing.T) {
	a := searchtest.Track("HUMBLE.", "Kendrick Lamar", domain.ProviderDeezer, nil)
	b := searchtest.Track("Humble", "Kendrick Lamar", domain.ProviderITunes, nil)
	entities := rank.Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1 (same canonical title, different providers)", len(entities))
	}
	if got := len(entities[0].Result.Sources); got != 2 {
		t.Errorf("sources = %d, want 2", got)
	}
	if entities[0].Result.Confidence != domain.ConfidenceMedium {
		t.Errorf("confidence = %v, want medium (multi-source text merge)", entities[0].Result.Confidence)
	}
}

func TestMerge_TyposStaySeparate(t *testing.T) {
	a := searchtest.Track("Bohemian Rhapsody", "Queen", domain.ProviderDeezer, nil)
	b := searchtest.Track("Bohemian Rapsody", "Queen", domain.ProviderITunes, nil)
	entities := rank.Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 2 {
		t.Fatalf("got %d entities, want 2 (no fuzzy merge)", len(entities))
	}
}

func TestMerge_DifferentArtistStaysSeparate(t *testing.T) {
	a := searchtest.Track("Crazy", "Gnarls Barkley", domain.ProviderDeezer, nil)
	b := searchtest.Track("Crazy", "Patsy Cline", domain.ProviderITunes, nil)
	entities := rank.Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 2 {
		t.Fatalf("got %d entities, want 2 (same title, different artists)", len(entities))
	}
}

func TestMerge_Artists(t *testing.T) {
	t.Run("same-name artists merge and union sources", func(t *testing.T) {
		a := searchtest.Res(domain.ResultKindArtist, "Drake", "", domain.ProviderDeezer, nil)
		b := searchtest.Res(domain.ResultKindArtist, "Drake", "", domain.ProviderITunes, nil)
		entities := rank.Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 1 {
			t.Fatalf("got %d entities, want 1", len(entities))
		}
		if got := len(entities[0].Result.Sources); got != 2 {
			t.Errorf("sources = %d, want 2", got)
		}
	})

	t.Run("distinct names stay separate", func(t *testing.T) {
		a := searchtest.Res(domain.ResultKindArtist, "Blink-182", "", domain.ProviderDeezer, nil)
		b := searchtest.Res(domain.ResultKindArtist, "Blink", "", domain.ProviderITunes, nil)
		entities := rank.Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 (Blink-182 != Blink)", len(entities))
		}
	})
}

func TestMerge_EmptyNormalizedTitleNeverNameMerges(t *testing.T) {
	t.Run("fully-bracketed track titles stay separate", func(t *testing.T) {
		a := searchtest.Track("(Intro)", "Same Artist", domain.ProviderDeezer, nil)
		b := searchtest.Track("(Outro)", "Same Artist", domain.ProviderITunes, nil)
		entities := rank.Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 (empty normalized titles must not merge)", len(entities))
		}
	})

	t.Run("symbol-only artist names stay separate", func(t *testing.T) {
		a := searchtest.Res(domain.ResultKindArtist, "!!!", "", domain.ProviderDeezer, nil)
		b := searchtest.Res(domain.ResultKindArtist, "†††", "", domain.ProviderITunes, nil)
		entities := rank.Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 (empty normalized names must not merge)", len(entities))
		}
	})
}

func TestMergeInto_KeepsStrongestResolutionTier(t *testing.T) {
	a := searchtest.WithISRC(searchtest.Track("HUMBLE.", "Kendrick Lamar", domain.ProviderDeezer, nil), "USUM71703089")
	b := searchtest.WithISRC(searchtest.Track("Humble", "Kendrick Lamar", domain.ProviderITunes, nil), "USUM71703089")
	c := searchtest.Track("Humble", "Kendrick Lamar", domain.ProviderTheAudioDB, nil)

	entities := rank.Merge([][]domain.SearchResult{{a}, {b}, {c}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(entities))
	}
	r := entities[0].Result
	if tier := r.ResolutionTier.Tier.String(); tier != "isrc" {
		t.Errorf("resolution_tier = %v, want isrc (name merge must not downgrade)", tier)
	}
	if r.Confidence != domain.ConfidenceHigh {
		t.Errorf("confidence = %v, want high (identity-proven)", r.Confidence)
	}
}

func moreCompleteTrack(title string, duration int) domain.SearchResult {
	r := withAlbum(searchtest.Track(title, "Some Artist", domain.ProviderDeezer, nil), "Some Album")
	r.ImageURL = "https://x/art.jpg"
	r.Duration = duration
	return r
}

func TestMergeInto_TitleTierCarriesTheOtherSidesISRCOnlyWhenItIsTheSameVersion(t *testing.T) {
	tests := []struct {
		name          string
		title, other  string
		dur, otherDur int
		wantISRCCarry bool
	}{
		{"titles agree under identity", "HUMBLE.", "Humble", 177, 177, true},
		{"one side has no known duration", "Song Title", "Song Title", 0, 200, true},
		{"6s on 200s is exactly 3%", "Song Title", "Song Title", 200, 206, true},
		{"5s sits exactly on the floor", "Song Title", "Song Title", 100, 105, true},
		{"8s on 200s exceeds 3%", "Song Title", "Song Title", 200, 208, false},
		{"6s on 100s exceeds the 5s floor", "Song Title", "Song Title", 100, 106, false},
		{"a qualifier differs at the same duration", "Fix You", "Fix You (Live)", 295, 295, false},
		{"a remix is a different version", "Drinking in L.A.", "Drinking in L.A. (Who Mix?)", 236, 307, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			other := searchtest.WithISRC(searchtest.Track(tt.other, "Some Artist", domain.ProviderSpotify, nil), "ISRC-OTHER")
			other.Duration = tt.otherDur

			entities := rank.Merge([][]domain.SearchResult{{moreCompleteTrack(tt.title, tt.dur)}, {other}})

			if len(entities) != 1 {
				t.Fatalf("got %d entities, want 1 (title tier still merges)", len(entities))
			}
			if carried := entities[0].Result.ISRC == "ISRC-OTHER"; carried != tt.wantISRCCarry {
				t.Errorf("ISRC = %q, want carried = %v", entities[0].Result.ISRC, tt.wantISRCCarry)
			}
		})
	}
}

func TestMergeInto_NameMatchNeverPastesLookalikeArtworkOverIdentity(t *testing.T) {
	identity := searchtest.WithISRC(searchtest.Track("No Idea", "Don Toliver", domain.ProviderSpotify, nil), "ISRC-DONTOLIVER")
	lookalike := searchtest.Track("No Idea", "Don Toliver", domain.ProviderDeezer, nil)
	lookalike.ImageURL = "https://wrong/lookalike.jpg"
	lookalike.ArtworkSource = "deezer"

	orders := map[string][][]domain.SearchResult{
		"identity_first":  {{identity}, {lookalike}},
		"lookalike_first": {{lookalike}, {identity}},
	}
	for name, order := range orders {
		entities := rank.Merge(order)
		if len(entities) != 1 {
			t.Fatalf("%s: got %d entities, want 1 (same title+artist name-merge)", name, len(entities))
		}
		got := entities[0].Result
		if got.ImageURL != "" {
			t.Errorf("%s: ImageURL = %q, want empty placeholder (never the look-alike cover)", name, got.ImageURL)
		}
		if got.ISRC != "ISRC-DONTOLIVER" {
			t.Errorf("%s: ISRC = %q, want the identity preserved", name, got.ISRC)
		}
	}
}

func TestMergeInto_StrongIDMergeAdoptsSiblingArtwork(t *testing.T) {
	a := searchtest.WithISRC(searchtest.Track("HUMBLE.", "Kendrick Lamar", domain.ProviderMusicBrainz, nil), "USUM71703089")
	b := searchtest.WithISRC(searchtest.Track("Humble", "Kendrick Lamar", domain.ProviderDeezer, nil), "USUM71703089")
	b.ImageURL = "https://cover/humble.jpg"
	b.ArtworkSource = "deezer"

	entities := rank.Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(entities))
	}
	if got := entities[0].Result.ImageURL; got != "https://cover/humble.jpg" {
		t.Errorf("ImageURL = %q, want the sibling cover adopted (ISRC-proven same entity)", got)
	}
	if got := entities[0].Result.ArtworkSource; got != "deezer" {
		t.Errorf("ArtworkSource = %q, want deezer (source travels with the adopted cover)", got)
	}
}

func TestMerge_BestRankTracksMinAcrossProviders(t *testing.T) {
	groupA := []domain.SearchResult{
		searchtest.Track("Filler One", "X", domain.ProviderDeezer, nil),
		searchtest.Track("Filler Two", "X", domain.ProviderDeezer, nil),
		searchtest.Track("Target", "Kendrick Lamar", domain.ProviderDeezer, map[string]any{"isrc": "ISRC1"}),
	}
	groupB := []domain.SearchResult{
		searchtest.Track("Target", "Kendrick Lamar", domain.ProviderITunes, map[string]any{"isrc": "ISRC1"}),
	}
	entities := rank.Merge([][]domain.SearchResult{groupA, groupB})
	e := findByTitle(t, entities, "Target")
	if e.BestRank[domain.ProviderDeezer] != 2 {
		t.Errorf("deezer best rank = %d, want 2", e.BestRank[domain.ProviderDeezer])
	}
	if e.BestRank[domain.ProviderITunes] != 0 {
		t.Errorf("itunes best rank = %d, want 0", e.BestRank[domain.ProviderITunes])
	}
}

func TestMerge_TitleTierCarriesTheAlbumISRCPastARejectedRemix(t *testing.T) {
	deezer := moreCompleteTrack("Drinking in L.A.", 236)
	remix := searchtest.WithISRC(searchtest.Track("Drinking in L.A. (Who Mix?)", "Some Artist", domain.ProviderSpotify, nil), "ISRC-REMIX")
	remix.Duration = 307
	album := searchtest.WithISRC(searchtest.Track("Drinking in L.A.", "Some Artist", domain.ProviderSpotify, nil), "ISRC-ALBUM")
	album.Duration = 236

	entities := rank.Merge([][]domain.SearchResult{{deezer}, {remix, album}})

	if len(entities) != 1 || entities[0].Result.ISRC != "ISRC-ALBUM" {
		t.Fatalf("entities = %d, ISRC = %q, want one entity carrying the album recording's ISRC, never the remix's", len(entities), entities[0].Result.ISRC)
	}
}

func TestMerge_TitleTierKeepsProvenIdentityOverALaterMoreCompleteCandidate(t *testing.T) {
	deezer := searchtest.WithISRC(searchtest.Track("Song Title", "Some Artist", domain.ProviderDeezer, nil), "ISRC-PROVEN")
	mb := searchtest.WithISRC(searchtest.Track("Song Title", "Some Artist", domain.ProviderSpotify, nil), "ISRC-PROVEN")
	extended := searchtest.Track("Song Title (Extended)", "Some Artist", domain.ProviderTheAudioDB, nil)
	extended.Album = "Some Album"
	extended.ImageURL = "https://x/art.jpg"
	extended.UPC = "000000000000"
	extended.Duration = 400

	entities := rank.Merge([][]domain.SearchResult{{deezer}, {mb}, {extended}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1 (title tier still merges)", len(entities))
	}
	r := entities[0].Result
	if r.ISRC != "ISRC-PROVEN" {
		t.Errorf("ISRC = %q, want ISRC-PROVEN kept (the proven side, not the merely more complete candidate)", r.ISRC)
	}
}

func TestMerge_DifferentISRCsKeepARemixApartFromTheOriginal(t *testing.T) {
	original := searchtest.WithISRC(searchtest.Track("Song", "X", domain.ProviderDeezer, nil), "USUM71700001")
	remix := searchtest.WithISRC(searchtest.Track("Song (Remix)", "X", domain.ProviderSpotify, nil), "USUM71700002")

	entities := rank.Merge([][]domain.SearchResult{{original}, {remix}})

	if len(entities) != 2 {
		t.Fatalf("got %d entities, want the remix and the original kept apart", len(entities))
	}
}

func TestMerge_DifferentISRCsStillMergeWhenTheVersionWordsAgree(t *testing.T) {
	a := searchtest.WithISRC(searchtest.Track("Song", "X", domain.ProviderDeezer, nil), "USUM71700001")
	b := searchtest.WithISRC(searchtest.Track("Song", "X", domain.ProviderSpotify, nil), "USUM71700002")

	entities := rank.Merge([][]domain.SearchResult{{a}, {b}})

	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(entities))
	}
}

func TestMerge_ExplicitAndCleanTwinsCarryTheISRCOfTheSameSource(t *testing.T) {
	explicit := searchtest.WithISRC(searchtest.Track("Song", "X", domain.ProviderDeezer, nil), "USUM71700001")
	explicit.Explicit = domain.BoolPtr(true)
	clean := searchtest.WithISRC(searchtest.Track("Song", "X", domain.ProviderSpotify, nil), "USUM71700002")
	clean.Album = "Clean Album"

	for _, order := range [][][]domain.SearchResult{{{explicit}, {clean}}, {{clean}, {explicit}}} {
		entities := rank.Merge(order)
		if len(entities) != 1 {
			t.Fatalf("got %d entities, want the twins merged", len(entities))
		}
		r := entities[0].Result
		if got, want := r.Explicit != nil && *r.Explicit, r.ISRC == "USUM71700001"; got != want {
			t.Errorf("ISRC %q with explicit=%v: the flag must come from the ISRC's own source", r.ISRC, got)
		}
	}
}

func TestMerge_ISRCsDifferingOnlyInFormattingMatch(t *testing.T) {
	soundcloud := searchtest.WithISRC(searchtest.Track("Song", "X", domain.ProviderSoundCloud, nil), "us-um7-17-03861")
	deezer := searchtest.WithISRC(searchtest.Track("Song (Remix)", "Y", domain.ProviderDeezer, nil), "USUM71703861")

	entities := rank.Merge([][]domain.SearchResult{{soundcloud}, {deezer}})

	if len(entities) != 1 {
		t.Fatalf("got %d entities, want the dashed and canonical ISRC to match", len(entities))
	}
}

type recordedDeezerTrack struct {
	id       string
	isrc     string
	explicit bool
}

var (
	recordedCleanEdit    = recordedDeezerTrack{id: "642332252", isrc: "QM42K1903228", explicit: false}
	recordedExplicitTwin = recordedDeezerTrack{id: "1216445232", isrc: "QM42K1903228", explicit: true}
)

func recordedDeezerResult(recorded recordedDeezerTrack, known bool) domain.SearchResult {
	r := searchtest.WithISRC(searchtest.Track("Been Waiting!", "Autumn!", domain.ProviderDeezer, nil), recorded.isrc)
	r.Album = "##R3 ##R3"
	r.Duration = 123
	r.Sources[0].ExternalID = recorded.id
	if known {
		r.Explicit = domain.BoolPtr(recorded.explicit)
	}
	return r
}

func TestMerge_ExplicitTrueWinsThenFalse(t *testing.T) {
	tests := []struct {
		name         string
		first        domain.SearchResult
		second       domain.SearchResult
		wantKnown    bool
		wantExplicit bool
	}{
		{"clean edit and its explicit twin", recordedDeezerResult(recordedCleanEdit, true), recordedDeezerResult(recordedExplicitTwin, true), true, true},
		{"clean edit and a provider that does not know", recordedDeezerResult(recordedCleanEdit, true), recordedDeezerResult(recordedExplicitTwin, false), true, false},
		{"explicit twin and a provider that does not know", recordedDeezerResult(recordedExplicitTwin, true), recordedDeezerResult(recordedCleanEdit, false), true, true},
		{"neither provider knows", recordedDeezerResult(recordedCleanEdit, false), recordedDeezerResult(recordedExplicitTwin, false), false, false},
	}
	for _, tt := range tests {
		for _, swapped := range []bool{false, true} {
			groups := [][]domain.SearchResult{{tt.first}, {tt.second}}
			if swapped {
				groups = [][]domain.SearchResult{{tt.second}, {tt.first}}
			}
			t.Run(tt.name, func(t *testing.T) {
				entities := rank.Merge(groups)
				if len(entities) != 1 {
					t.Fatalf("got %d entities, want the recorded pair merged", len(entities))
				}
				got := entities[0].Result.Explicit
				if (got != nil) != tt.wantKnown || (got != nil && *got != tt.wantExplicit) {
					t.Errorf("merged Explicit = %v, want known=%v explicit=%v (swapped=%v)", got, tt.wantKnown, tt.wantExplicit, swapped)
				}
			})
		}
	}
}

func artistWithID(name string, provider domain.ProviderName, id string, fans int64, image string) domain.SearchResult {
	return domain.SearchResult{
		Kind:     domain.ResultKindArtist,
		Title:    name,
		ImageURL: image,
		FanCount: fans,
		Sources:  []domain.ProviderLink{{Provider: provider, ExternalID: id}},
	}
}

func entityWithSource(t *testing.T, entities []rank.Entity, provider domain.ProviderName, id string) rank.Entity {
	t.Helper()
	for _, e := range entities {
		for _, s := range e.Result.Sources {
			if s.Provider == provider && s.ExternalID == id {
				return e
			}
		}
	}
	t.Fatalf("no entity holds %s:%s", provider, id)
	return rank.Entity{}
}

func TestMerge_SameNameArtistsWithDifferentIDsFromOneProviderStayApart(t *testing.T) {
	entities := rank.Merge([][]domain.SearchResult{{
		artistWithID("Michael Jackson", domain.ProviderDeezer, "373427811", 211, "https://img/fan.jpg"),
		artistWithID("Michael Jackson", domain.ProviderDeezer, "259", 13187101, "https://img/mj.jpg"),
	}})

	if len(entities) != 2 {
		t.Fatalf("got %d entities, want 2: two Deezer ids are two different artists", len(entities))
	}
}

func TestMerge_AnotherProviderJoinsTheMostPopularSameNameArtist(t *testing.T) {
	entities := rank.Merge([][]domain.SearchResult{
		{
			artistWithID("Michael Jackson", domain.ProviderDeezer, "373427811", 211, "https://img/fan.jpg"),
			artistWithID("Michael Jackson", domain.ProviderDeezer, "259", 13187101, "https://img/mj.jpg"),
		},
		{artistWithID("Michael Jackson", domain.ProviderSpotify, "3fMbdgg4jU18AjLCKBhRSm", 0, "https://img/sp.jpg")},
	})

	real := entityWithSource(t, entities, domain.ProviderDeezer, "259")
	if len(real.Result.Sources) != 2 {
		t.Fatalf("the 13M-fan artist has sources %v, want Spotify to join it", real.Result.Sources)
	}
	if fan := entityWithSource(t, entities, domain.ProviderDeezer, "373427811"); len(fan.Result.Sources) != 1 {
		t.Errorf("the 211-fan account took sources %v, want only its own", fan.Result.Sources)
	}
}

func TestMerge_KeepsTheLargestFanCount(t *testing.T) {
	entities := rank.Merge([][]domain.SearchResult{
		{artistWithID("Drake", domain.ProviderAppleMusic, "271256", 5, "https://img/apple.jpg")},
		{artistWithID("Drake", domain.ProviderDeezer, "246791", 24104376, "")},
	})

	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(entities))
	}
	if got := entities[0].Result.FanCount; got != 24104376 {
		t.Errorf("FanCount = %d, want the largest, 24104376", got)
	}
}

func TestMerge_ReuploadsOfOneTrackOnOneProviderStillFold(t *testing.T) {
	first := searchtest.Res(domain.ResultKindTrack, "Smooth Criminal", "Michael Jackson", domain.ProviderSoundCloud, nil)
	second := searchtest.Res(domain.ResultKindTrack, "Smooth Criminal", "Michael Jackson", domain.ProviderSoundCloud, nil)
	second.Sources[0].ExternalID = "another-upload"

	if entities := rank.Merge([][]domain.SearchResult{{first, second}}); len(entities) != 1 {
		t.Fatalf("got %d entities, want 1: the same-provider rule is for artists only", len(entities))
	}
}

func TestMerge_TheMergedArtistKeepsSpotifysTopPick(t *testing.T) {
	picked := artistWithID("Cochise", domain.ProviderSpotify, "sp1", 0, "https://img/sp.jpg")
	picked.Extras = map[string]any{domain.ExtraTopPick: true}

	entities := rank.Merge([][]domain.SearchResult{
		{artistWithID("Cochise", domain.ProviderDeezer, "dz1", 500000, "https://img/dz.jpg")},
		{picked},
	})

	merged := entityWithSource(t, entities, domain.ProviderDeezer, "dz1")
	if len(merged.Result.Sources) != 2 || !domain.BoolExtra(merged.Result.Extras, domain.ExtraTopPick) {
		t.Fatalf("merged artist = sources %v extras %v, want Spotify joined and the top pick kept", merged.Result.Sources, merged.Result.Extras)
	}
}

func TestMerge_ASongCreditedWithExtraNamesFoldsIntoTheLeadArtistsSong(t *testing.T) {
	entities := rank.Merge([][]domain.SearchResult{
		{searchtest.Track("Yacht Club (feat. Juice WRLD)", "Lil Yachty", domain.ProviderDeezer, nil)},
		{searchtest.Track("Yacht Club (feat. Juice WRLD)", "Lil Yachty, RD, Lil Boat", domain.ProviderSoundCloud, nil)},
	})

	if len(entities) != 1 {
		t.Fatalf("got %d entities, want one Yacht Club", len(entities))
	}
}

func TestMerge_AnAlbumWithACoCreditFoldsIntoTheLeadArtistsAlbum(t *testing.T) {
	entities := rank.Merge([][]domain.SearchResult{
		{searchtest.Res(domain.ResultKindAlbum, "Her Loss", "Drake & 21 Savage", domain.ProviderAppleMusic, nil)},
		{searchtest.Res(domain.ResultKindAlbum, "Her Loss", "Drake", domain.ProviderSpotify, nil)},
	})

	if len(entities) != 1 {
		t.Fatalf("got %d entities, want one Her Loss", len(entities))
	}
}

func TestMerge_SameTitleByADifferentLeadArtistStaysApart(t *testing.T) {
	entities := rank.Merge([][]domain.SearchResult{
		{searchtest.Track("Cochise", "Audioslave", domain.ProviderDeezer, nil)},
		{searchtest.Track("Cochise", "Cochise, Audioslave", domain.ProviderSoundCloud, nil)},
	})

	if len(entities) != 2 {
		t.Fatalf("got %d entities, want the two songs apart", len(entities))
	}
}

func TestMerge_TheCatalogCreditBeatsASoundCloudAccountName(t *testing.T) {
	upload := searchtest.Track("Yacht Club (feat. Juice WRLD)", "Lil Yachty, RD, Lil Boat", domain.ProviderSoundCloud, nil)
	upload.ImageURL, upload.Duration, upload.Album = "https://img/sc.jpg", 180, "Lil Boat"

	for _, order := range [][][]domain.SearchResult{
		{{searchtest.Track("Yacht Club (feat. Juice WRLD)", "Lil Yachty", domain.ProviderDeezer, nil)}, {upload}},
		{{upload}, {searchtest.Track("Yacht Club (feat. Juice WRLD)", "Lil Yachty", domain.ProviderDeezer, nil)}},
	} {
		entities := rank.Merge(order)
		if len(entities) != 1 {
			t.Fatalf("got %d entities, want one Yacht Club", len(entities))
		}
		if got := entities[0].Result.Subtitle; got != "Lil Yachty" {
			t.Errorf("subtitle %q, want the catalog credit Lil Yachty", got)
		}
	}
}

func TestMerge_SymbolOnlyArtistNamesStillMatch(t *testing.T) {
	entities := rank.Merge([][]domain.SearchResult{
		{artistWithID("¥$", domain.ProviderDeezer, "dz", 900000, "https://img/dz.jpg")},
		{artistWithID("¥$", domain.ProviderSpotify, "sp", 0, "https://img/sp.jpg")},
	})

	if len(entities) != 1 {
		t.Fatalf("got %d entities, want the Deezer and Spotify ¥$ as one", len(entities))
	}
}

func TestMerge_DifferentSymbolOnlyNamesStayApart(t *testing.T) {
	entities := rank.Merge([][]domain.SearchResult{
		{artistWithID("¥$", domain.ProviderDeezer, "dz", 900000, "")},
		{artistWithID("$$$", domain.ProviderSpotify, "sp", 0, "")},
	})

	if len(entities) != 2 {
		t.Fatalf("got %d entities, want ¥$ and $$$ apart", len(entities))
	}
}

func TestMerge_AnotherProviderJoinsTheMostPopularSameNameArtistWhicheverComesFirst(t *testing.T) {
	entities := rank.Merge([][]domain.SearchResult{
		{artistWithID("Michael Jackson", domain.ProviderSpotify, "3fMbdgg4jU18AjLCKBhRSm", 0, "https://img/sp.jpg")},
		{
			artistWithID("Michael Jackson", domain.ProviderDeezer, "373427811", 211, "https://img/fan.jpg"),
			artistWithID("Michael Jackson", domain.ProviderDeezer, "259", 13187101, "https://img/mj.jpg"),
		},
	})

	if real := entityWithSource(t, entities, domain.ProviderDeezer, "259"); len(real.Result.Sources) != 2 {
		t.Fatalf("the 13M-fan artist has sources %v, want Spotify to join it", real.Result.Sources)
	}
	if fan := entityWithSource(t, entities, domain.ProviderDeezer, "373427811"); len(fan.Result.Sources) != 1 {
		t.Errorf("the 211-fan account took sources %v, want only its own", fan.Result.Sources)
	}
}

func explicitProbeTrack(provider domain.ProviderName, id string, explicit *bool) domain.SearchResult {
	r := searchtest.WithISRC(searchtest.Track("Been Waiting!", "Autumn!", provider, nil), "QM42K1903228")
	r.Album = "##R3 ##R3"
	r.Duration = 123
	r.Sources[0].ExternalID = id
	r.Explicit = explicit
	return r
}

func everyOrderOf3(a, b, c domain.SearchResult) [][][]domain.SearchResult {
	return [][][]domain.SearchResult{
		{{a}, {b}, {c}},
		{{a}, {c}, {b}},
		{{b}, {a}, {c}},
		{{b}, {c}, {a}},
		{{c}, {a}, {b}},
		{{c}, {b}, {a}},
	}
}

func TestMerge_ThreeProvidersExplicitIsTrueWhenAnySaysTrueInEveryOrder(t *testing.T) {
	clean := explicitProbeTrack(domain.ProviderDeezer, "642332252", domain.BoolPtr(false))
	unknown := explicitProbeTrack(domain.ProviderAppleMusic, "a1", nil)
	explicit := explicitProbeTrack(domain.ProviderSpotify, "s1", domain.BoolPtr(true))

	for i, groups := range everyOrderOf3(clean, unknown, explicit) {
		entities := rank.Merge(groups)
		if len(entities) != 1 {
			t.Fatalf("order %d: got %d entities, want the three providers merged", i, len(entities))
		}
		if got := entities[0].Result.Explicit; got == nil || !*got {
			t.Errorf("order %d: merged Explicit = %v, want true (one provider says explicit)", i, got)
		}
	}
}

func TestMerge_ThreeProvidersExplicitIsFalseWhenEveryKnownValueIsFalseInEveryOrder(t *testing.T) {
	clean := explicitProbeTrack(domain.ProviderDeezer, "642332252", domain.BoolPtr(false))
	alsoClean := explicitProbeTrack(domain.ProviderSpotify, "s1", domain.BoolPtr(false))
	unknown := explicitProbeTrack(domain.ProviderAppleMusic, "a1", nil)

	for i, groups := range everyOrderOf3(clean, alsoClean, unknown) {
		entities := rank.Merge(groups)
		if len(entities) != 1 {
			t.Fatalf("order %d: got %d entities, want the three providers merged", i, len(entities))
		}
		if got := entities[0].Result.Explicit; got == nil || *got {
			t.Errorf("order %d: merged Explicit = %v, want a known false", i, got)
		}
	}
}

func TestMerge_ThreeProvidersNoneKnowingLeavesExplicitUnknownInEveryOrder(t *testing.T) {
	a := explicitProbeTrack(domain.ProviderDeezer, "642332252", nil)
	b := explicitProbeTrack(domain.ProviderSpotify, "s1", nil)
	c := explicitProbeTrack(domain.ProviderAppleMusic, "a1", nil)

	for i, groups := range everyOrderOf3(a, b, c) {
		if got := rank.Merge(groups)[0].Result.Explicit; got != nil {
			t.Errorf("order %d: merged Explicit = %v, want nil when no provider knows", i, *got)
		}
	}
}

func TestMerge_CleanTwinWithAKnownFalseKeepsTheFlagOfItsOwnISRC(t *testing.T) {
	explicit := searchtest.WithISRC(searchtest.Track("Song", "X", domain.ProviderDeezer, nil), "USUM71700001")
	explicit.Explicit = domain.BoolPtr(true)
	clean := searchtest.WithISRC(searchtest.Track("Song", "X", domain.ProviderSpotify, nil), "USUM71700002")
	clean.Album = "Clean Album"
	clean.Explicit = domain.BoolPtr(false)

	for i, order := range [][][]domain.SearchResult{{{explicit}, {clean}}, {{clean}, {explicit}}} {
		entities := rank.Merge(order)
		if len(entities) != 1 {
			t.Fatalf("order %d: got %d entities, want the twins merged", i, len(entities))
		}
		r := entities[0].Result
		if r.Explicit == nil {
			t.Fatalf("order %d: merged Explicit is unknown, want the known flag of ISRC %q", i, r.ISRC)
		}
		if want := r.ISRC == "USUM71700001"; *r.Explicit != want {
			t.Errorf("order %d: ISRC %q carries explicit=%v, want %v (the ISRC's own source)", i, r.ISRC, *r.Explicit, want)
		}
	}
}
