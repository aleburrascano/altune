package rank

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"testing"
)

func TestMerge_CapsEachProviderGroupBeforeTheQuadraticScan(t *testing.T) {
	huge := make([]domain.SearchResult, 10000)
	for i := range huge {
		huge[i] = discoverytest.Track("title "+string(rune(0x4e00+i)), "artist "+string(rune(0x4e00+i)), domain.ProviderDeezer, nil)
	}
	entities := Merge([][]domain.SearchResult{huge})
	if len(entities) != maxMergeResultsPerProvider {
		t.Fatalf("got %d entities, want %d", len(entities), maxMergeResultsPerProvider)
	}
}

func TestMerge_ScansOnlyTheCappedGroup(t *testing.T) {
	huge := make([]domain.SearchResult, 10000)
	for i := range huge {
		huge[i] = discoverytest.Track("title "+string(rune(0x4e00+i)), "artist "+string(rune(0x4e00+i)), domain.ProviderDeezer, nil)
	}
	comparisons := 0
	mergeObserving([][]domain.SearchResult{huge}, func() { comparisons++ })
	if limit := maxMergeResultsPerProvider * maxMergeResultsPerProvider; comparisons > limit {
		t.Fatalf("scan made %d comparisons, want at most %d (the cap must run before the scan)", comparisons, limit)
	}
}

func findByTitle(t *testing.T, entities []Entity, title string) Entity {
	t.Helper()
	for _, e := range entities {
		if e.Result.Title == title {
			return e
		}
	}
	t.Fatalf("no entity titled %q in %d entities", title, len(entities))
	return Entity{}
}

func TestMerge_AmbiguousArtistNameKeepsIdentitiesSeparate(t *testing.T) {
	mb1 := discoverytest.WithMBID(discoverytest.Res(domain.ResultKindArtist, "Che", "", domain.ProviderMusicBrainz, nil), "mbid-1")
	mb2 := discoverytest.WithMBID(discoverytest.Res(domain.ResultKindArtist, "Che", "", domain.ProviderMusicBrainz, nil), "mbid-2")
	itunes := discoverytest.Res(domain.ResultKindArtist, "Che", "", domain.ProviderITunes, nil)

	entities := Merge([][]domain.SearchResult{{mb1, mb2}, {itunes}})

	if len(entities) != 3 {
		t.Fatalf("got %d entities, want 3 (two MB identities + unmerged iTunes)", len(entities))
	}
	for _, e := range entities {
		if got := len(e.Result.Sources); got != 1 {
			t.Errorf("entity %q has %d sources, want 1 (no cross-identity union)", e.Result.Title, got)
		}
	}
}

func TestMerge_UnambiguousArtistNameStillNameMerges(t *testing.T) {
	mb := discoverytest.WithMBID(discoverytest.Res(domain.ResultKindArtist, "Drake", "", domain.ProviderMusicBrainz, nil), "mbid-drake")
	itunes := discoverytest.Res(domain.ResultKindArtist, "Drake", "", domain.ProviderITunes, nil)

	entities := Merge([][]domain.SearchResult{{mb}, {itunes}})

	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1 (name merge preserved when unambiguous)", len(entities))
	}
	if got := len(entities[0].Result.Sources); got != 2 {
		t.Errorf("merged entity has %d sources, want 2", got)
	}
}

func TestMerge_ITunesBridgesIntoMBIdentityDespiteAmbiguousName(t *testing.T) {
	mb := domain.SearchResult{
		Kind:    domain.ResultKindArtist,
		Title:   "Che",
		Sources: []domain.SourceRef{{Provider: domain.ProviderMusicBrainz, ExternalID: "mbid-che-1", URL: "https://mb/1"}},
		MBID:    "mbid-che-1",
		Xref:    map[domain.ProviderKey]string{"itunes": "5468295"},
	}
	mb2 := domain.SearchResult{
		Kind:    domain.ResultKindArtist,
		Title:   "Che",
		Sources: []domain.SourceRef{{Provider: domain.ProviderMusicBrainz, ExternalID: "mbid-che-2", URL: "https://mb/2"}},
		MBID:    "mbid-che-2",
	}
	itunes := domain.SearchResult{
		Kind:    domain.ResultKindArtist,
		Title:   "Che",
		Sources: []domain.SourceRef{{Provider: domain.ProviderITunes, ExternalID: "5468295", URL: "https://itunes/x"}},
	}

	entities := Merge([][]domain.SearchResult{{mb, mb2}, {itunes}})

	if len(entities) != 2 {
		t.Fatalf("got %d entities, want 2 (iTunes bridged into MB#1; MB#2 stays separate)", len(entities))
	}
	bridged := findByTitle(t, entities, "Che")
	hasITunes := false
	for _, s := range bridged.Result.Sources {
		if s.Provider == domain.ProviderITunes {
			hasITunes = true
		}
	}
	if !hasITunes {
		t.Error("iTunes source should have bridged into the MB identity via xref[itunes]")
	}
}

func TestMerge_IdentifierMatch(t *testing.T) {
	t.Run("same isrc merges across providers", func(t *testing.T) {
		a := discoverytest.WithISRC(discoverytest.Track("HUMBLE.", "Kendrick Lamar", domain.ProviderDeezer, nil), "USUM71703089")
		b := discoverytest.WithISRC(discoverytest.Track("Humble", "Kendrick Lamar", domain.ProviderITunes, nil), "USUM71703089")
		entities := Merge([][]domain.SearchResult{{a}, {b}})
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

	t.Run("distinct mbid stays separate", func(t *testing.T) {
		a := discoverytest.WithMBID(discoverytest.Track("Intro", "Artist A", domain.ProviderMusicBrainz, nil), "mbid-1")
		b := discoverytest.WithMBID(discoverytest.Track("Intro", "Artist A", domain.ProviderMusicBrainz, nil), "mbid-2")
		entities := Merge([][]domain.SearchResult{{a, b}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 (distinct MBIDs)", len(entities))
		}
	})
}

func TestMerge_AlbumUPCTier(t *testing.T) {
	t.Run("same upc merges albums across providers", func(t *testing.T) {
		a := discoverytest.Res(domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", domain.ProviderAppleMusic, nil)
		a.UPC = "00602557618280"
		b := discoverytest.Res(domain.ResultKindAlbum, "DAMN. (Deluxe)", "Kendrick Lamar", domain.ProviderDeezer, nil)
		b.UPC = "00602557618280"
		entities := Merge([][]domain.SearchResult{{a}, {b}})
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
		a := discoverytest.Res(domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", domain.ProviderAppleMusic, nil)
		a.UPC = "00602557618280"
		b := discoverytest.Res(domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", domain.ProviderDeezer, nil)
		b.UPC = "00602557618297"
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 1 {
			t.Fatalf("got %d entities, want 1 (upc mismatch must not block)", len(entities))
		}
	})

	t.Run("upc never merges tracks", func(t *testing.T) {
		a := discoverytest.Track("Same Barcode", "Artist A", domain.ProviderDeezer, nil)
		a.UPC = "00602557618280"
		b := discoverytest.Track("Different Title", "Artist B", domain.ProviderITunes, nil)
		b.UPC = "00602557618280"
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 (upc tier is album-only)", len(entities))
		}
	})
}

func TestMergeInto_CoalescesTypedContentFields(t *testing.T) {
	a := discoverytest.Track("HUMBLE.", "Kendrick Lamar", domain.ProviderDeezer, nil)
	a.Album = "DAMN."
	a.Duration = 177
	a.DeezerAlbumID = "13114014"
	b := discoverytest.Track("HUMBLE.", "Kendrick Lamar", domain.ProviderITunes, nil)
	entities := Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(entities))
	}
	r := entities[0].Result
	if r.Album != "DAMN." || r.Duration != 177 || r.DeezerAlbumID != "13114014" {
		t.Errorf("typed content fields dropped on merge: album=%q duration=%d deezerAlbumID=%q",
			r.Album, r.Duration, r.DeezerAlbumID)
	}
}

func TestMerge_IdentityBridge(t *testing.T) {
	mb := domain.SearchResult{
		Kind:    domain.ResultKindArtist,
		Title:   "Ye",
		Sources: []domain.SourceRef{{Provider: domain.ProviderMusicBrainz, ExternalID: "mbid-ye"}},
		MBID:    "mbid-ye",
		Xref:    map[domain.ProviderKey]string{"deezer": "230"},
	}
	dz := domain.SearchResult{
		Kind:    domain.ResultKindArtist,
		Title:   "Kanye West",
		Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "230"}},
	}

	t.Run("stated cross-provider id merges differing titles", func(t *testing.T) {
		entities := Merge([][]domain.SearchResult{{mb}, {dz}})
		if len(entities) != 1 {
			t.Fatalf("got %d entities, want 1 (bridged identity merge)", len(entities))
		}
		if tier := entities[0].Result.ResolutionTier.Tier.String(); tier != "bridge" {
			t.Errorf("resolution_tier = %v, want bridge", tier)
		}
		if entities[0].Result.Confidence != domain.ConfidenceHigh {
			t.Errorf("confidence = %v, want high (identity-grade)", entities[0].Result.Confidence)
		}
		if got := len(entities[0].Result.Sources); got != 2 {
			t.Errorf("sources = %d, want 2 (unioned)", got)
		}
	})

	t.Run("without the stated id they stay separate", func(t *testing.T) {
		bare := mb
		bare.Xref = nil
		entities := Merge([][]domain.SearchResult{{bare}, {dz}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 — no stated id, differing titles must not merge", len(entities))
		}
	})

	t.Run("a non-matching stated id does not merge", func(t *testing.T) {
		wrong := mb
		wrong.Xref = map[domain.ProviderKey]string{"deezer": "999"}
		entities := Merge([][]domain.SearchResult{{wrong}, {dz}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 — a mismatched stated id must not merge", len(entities))
		}
	})
}

func TestMerge_SequelStaysSeparate(t *testing.T) {
	a := discoverytest.Track("Shotta Flow", "NLE Choppa", domain.ProviderDeezer, map[string]any{"popularity": 90.0})
	b := discoverytest.Track("Shotta Flow 2", "NLE Choppa", domain.ProviderDeezer, map[string]any{"popularity": 40.0})
	entities := Merge([][]domain.SearchResult{{a, b}})
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
		a := discoverytest.Track(tc.a, "Some Artist", domain.ProviderDeezer, nil)
		b := discoverytest.Track(tc.b, "Some Artist", domain.ProviderITunes, nil)
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 1 {
			t.Errorf("%q + %q: got %d entities, want 1 (parenthetical variant folds in)", tc.a, tc.b, len(entities))
		}
	}
}

func TestMerge_DashAndParenVariantsStaySeparate(t *testing.T) {
	a := discoverytest.Track("Big Poppa - 2005 Remaster", "The Notorious B.I.G.", domain.ProviderLastFM, nil)
	b := discoverytest.Track("Big Poppa (2007 Remaster)", "The Notorious B.I.G.", domain.ProviderDeezer, nil)
	entities := Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 2 {
		t.Fatalf("got %d entities, want 2 — the dash-form variant must stay distinct", len(entities))
	}
}

func TestMerge_SameTitleAcrossProvidersMerges(t *testing.T) {
	a := discoverytest.Track("HUMBLE.", "Kendrick Lamar", domain.ProviderDeezer, nil)
	b := discoverytest.Track("Humble", "Kendrick Lamar", domain.ProviderITunes, nil)
	entities := Merge([][]domain.SearchResult{{a}, {b}})
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
	a := discoverytest.Track("Bohemian Rhapsody", "Queen", domain.ProviderDeezer, nil)
	b := discoverytest.Track("Bohemian Rapsody", "Queen", domain.ProviderITunes, nil)
	entities := Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 2 {
		t.Fatalf("got %d entities, want 2 (no fuzzy merge)", len(entities))
	}
}

func TestMerge_DifferentArtistStaysSeparate(t *testing.T) {
	a := discoverytest.Track("Crazy", "Gnarls Barkley", domain.ProviderDeezer, nil)
	b := discoverytest.Track("Crazy", "Patsy Cline", domain.ProviderITunes, nil)
	entities := Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 2 {
		t.Fatalf("got %d entities, want 2 (same title, different artists)", len(entities))
	}
}

func TestMerge_Artists(t *testing.T) {
	t.Run("same-name artists merge and union sources", func(t *testing.T) {
		a := discoverytest.Res(domain.ResultKindArtist, "Drake", "", domain.ProviderDeezer, nil)
		b := discoverytest.Res(domain.ResultKindArtist, "Drake", "", domain.ProviderITunes, nil)
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 1 {
			t.Fatalf("got %d entities, want 1", len(entities))
		}
		if got := len(entities[0].Result.Sources); got != 2 {
			t.Errorf("sources = %d, want 2", got)
		}
	})

	t.Run("distinct names stay separate", func(t *testing.T) {
		a := discoverytest.Res(domain.ResultKindArtist, "Blink-182", "", domain.ProviderDeezer, nil)
		b := discoverytest.Res(domain.ResultKindArtist, "Blink", "", domain.ProviderITunes, nil)
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 (Blink-182 != Blink)", len(entities))
		}
	})
}

func TestAmbiguousArtistNames_OnlyMusicBrainzMBIDsCount(t *testing.T) {
	mb := discoverytest.WithMBID(discoverytest.Res(domain.ResultKindArtist, "Queen", "", domain.ProviderMusicBrainz, nil), "mbid-current")
	lastfm := discoverytest.WithMBID(discoverytest.Res(domain.ResultKindArtist, "Queen", "", domain.ProviderLastFM, nil), "mbid-stale")

	if got := ambiguousArtistNamesFlat([]domain.SearchResult{mb, lastfm}); got["queen"] {
		t.Error("MB + stale Last.fm mbid marked the name ambiguous — only MusicBrainz MBIDs may count")
	}

	mb2 := discoverytest.WithMBID(discoverytest.Res(domain.ResultKindArtist, "Queen", "", domain.ProviderMusicBrainz, nil), "mbid-second")
	if got := ambiguousArtistNamesFlat([]domain.SearchResult{mb, mb2}); !got["queen"] {
		t.Error("two genuine MB identities with distinct MBIDs must still mark the name ambiguous")
	}
}

func TestMerge_UPCTierRefusesConflictingMBIDs(t *testing.T) {
	mkAlbum := func(title, mbid string, provider domain.ProviderName) domain.SearchResult {
		r := discoverytest.WithMBID(discoverytest.Res(domain.ResultKindAlbum, title, "Kendrick Lamar", provider, nil), mbid)
		r.UPC = "00602557618280"
		return r
	}
	a := mkAlbum("DAMN.", "mbid-1", domain.ProviderAppleMusic)
	b := mkAlbum("DAMN. (Deluxe)", "mbid-2", domain.ProviderDeezer)
	c := discoverytest.WithMBID(discoverytest.Res(domain.ResultKindAlbum, "DAMN. (Deluxe Edition)", "Kendrick Lamar", domain.ProviderMusicBrainz, nil), "mbid-2")

	for name, order := range map[string][]domain.SearchResult{
		"a_b_c": {a, b, c},
		"c_b_a": {c, b, a},
	} {
		entities := Merge([][]domain.SearchResult{order})
		if len(entities) != 2 {
			t.Errorf("order %s: got %d entities, want 2 (conflicting MBIDs must not UPC-merge)", name, len(entities))
		}
	}
}

func TestMerge_EmptyNormalizedTitleNeverNameMerges(t *testing.T) {
	t.Run("fully-bracketed track titles stay separate", func(t *testing.T) {
		a := discoverytest.Track("(Intro)", "Same Artist", domain.ProviderDeezer, nil)
		b := discoverytest.Track("(Outro)", "Same Artist", domain.ProviderITunes, nil)
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 (empty normalized titles must not merge)", len(entities))
		}
	})

	t.Run("symbol-only artist names stay separate", func(t *testing.T) {
		a := discoverytest.Res(domain.ResultKindArtist, "!!!", "", domain.ProviderDeezer, nil)
		b := discoverytest.Res(domain.ResultKindArtist, "†††", "", domain.ProviderITunes, nil)
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 (empty normalized names must not merge)", len(entities))
		}
	})
}

func TestMergeInto_KeepsStrongestResolutionTier(t *testing.T) {
	a := discoverytest.WithISRC(discoverytest.Track("HUMBLE.", "Kendrick Lamar", domain.ProviderDeezer, nil), "USUM71703089")
	b := discoverytest.WithISRC(discoverytest.Track("Humble", "Kendrick Lamar", domain.ProviderITunes, nil), "USUM71703089")
	c := discoverytest.Track("Humble", "Kendrick Lamar", domain.ProviderLastFM, nil)

	entities := Merge([][]domain.SearchResult{{a}, {b}, {c}})
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

func TestMergeInto_TitleTierNeverImportsARemixMBIDOntoTheOriginal(t *testing.T) {
	original := discoverytest.WithISRC(discoverytest.Track("Drinking in L.A.", "Bran Van 3000", domain.ProviderDeezer, nil), "CAA509814003")
	original.Duration = 236
	remix := discoverytest.WithMBID(discoverytest.Track("Drinking in L.A. (Who Mix?)", "Bran Van 3000", domain.ProviderMusicBrainz, nil), "d3dc2825")
	remix.Duration = 307

	entities := Merge([][]domain.SearchResult{{original}, {remix}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1 (title tier still merges)", len(entities))
	}
	r := entities[0].Result
	if r.MBID == "d3dc2825" {
		t.Errorf("MBID = %v, want anything but the remix's MBID", r.MBID)
	}
	if r.ISRC != "CAA509814003" {
		t.Errorf("ISRC = %v, want the original's CAA509814003 preserved", r.ISRC)
	}
}

func TestMergeInto_TitleTierCarriesMBIDAndISRCWhenTitlesAgreeUnderIdentity(t *testing.T) {
	a := discoverytest.WithISRC(discoverytest.Track("HUMBLE.", "Kendrick Lamar", domain.ProviderDeezer, nil), "USUM71703089")
	a.Duration = 177
	b := discoverytest.WithMBID(discoverytest.Track("Humble", "Kendrick Lamar", domain.ProviderMusicBrainz, nil), "mbid-humble")
	b.Duration = 177

	entities := Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(entities))
	}
	r := entities[0].Result
	if r.ISRC != "USUM71703089" {
		t.Errorf("ISRC = %v, want USUM71703089 carried across", r.ISRC)
	}
	if r.MBID != "mbid-humble" {
		t.Errorf("MBID = %v, want mbid-humble carried across", r.MBID)
	}
}

func TestMergeInto_TitleTierCarriesWhenOneSideHasNoKnownDuration(t *testing.T) {
	a := discoverytest.WithISRC(discoverytest.Track("Song Title", "Some Artist", domain.ProviderDeezer, nil), "ISRC-UNKNOWN-DUR")
	b := discoverytest.WithMBID(discoverytest.Track("Song Title", "Some Artist", domain.ProviderMusicBrainz, nil), "MBID-UNKNOWN-DUR")
	b.Duration = 200

	entities := Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(entities))
	}
	r := entities[0].Result
	if r.ISRC != "ISRC-UNKNOWN-DUR" {
		t.Errorf("ISRC = %v, want ISRC-UNKNOWN-DUR carried (a side has no known duration to disagree)", r.ISRC)
	}
	if r.MBID != "MBID-UNKNOWN-DUR" {
		t.Errorf("MBID = %v, want MBID-UNKNOWN-DUR carried (a side has no known duration to disagree)", r.MBID)
	}
}

func TestMergeInto_TitleTierCarriesAtThreePercentDurationBoundary(t *testing.T) {
	a := discoverytest.WithISRC(discoverytest.Track("Song Title", "Some Artist", domain.ProviderDeezer, nil), "ISRC-AT-BOUNDARY")
	a.Duration = 200
	b := discoverytest.WithMBID(discoverytest.Track("Song Title", "Some Artist", domain.ProviderMusicBrainz, nil), "MBID-AT-BOUNDARY")
	b.Duration = 206

	entities := Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(entities))
	}
	r := entities[0].Result
	if r.ISRC != "ISRC-AT-BOUNDARY" {
		t.Errorf("ISRC = %v, want ISRC-AT-BOUNDARY carried (6s on 200s is exactly 3%%, still within tolerance)", r.ISRC)
	}
	if r.MBID != "MBID-AT-BOUNDARY" {
		t.Errorf("MBID = %v, want MBID-AT-BOUNDARY carried (6s on 200s is exactly 3%%, still within tolerance)", r.MBID)
	}
}

func TestMergeInto_TitleTierCarriesAtFiveSecondFloorBoundary(t *testing.T) {
	a := discoverytest.WithISRC(discoverytest.Track("Song Title", "Some Artist", domain.ProviderDeezer, nil), "ISRC-AT-FLOOR")
	a.Duration = 100
	b := discoverytest.WithMBID(discoverytest.Track("Song Title", "Some Artist", domain.ProviderMusicBrainz, nil), "MBID-AT-FLOOR")
	b.Duration = 105

	entities := Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(entities))
	}
	r := entities[0].Result
	if r.ISRC != "ISRC-AT-FLOOR" {
		t.Errorf("ISRC = %v, want ISRC-AT-FLOOR carried (5s diff sits exactly on the 5s floor, still within tolerance)", r.ISRC)
	}
	if r.MBID != "MBID-AT-FLOOR" {
		t.Errorf("MBID = %v, want MBID-AT-FLOOR carried (5s diff sits exactly on the 5s floor, still within tolerance)", r.MBID)
	}
}

func TestMergeInto_TitleTierDoesNotCarryBeyondThreePercentDurationTolerance(t *testing.T) {
	a := discoverytest.WithISRC(discoverytest.Track("Song Title", "Some Artist", domain.ProviderDeezer, nil), "ISRC-OVER-TOLERANCE")
	a.Duration = 200
	b := discoverytest.WithMBID(discoverytest.Track("Song Title", "Some Artist", domain.ProviderMusicBrainz, nil), "MBID-OVER-TOLERANCE")
	b.Duration = 208

	entities := Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1 (title tier still merges)", len(entities))
	}
	r := entities[0].Result
	if r.MBID == "MBID-OVER-TOLERANCE" {
		t.Errorf("MBID = %v, want it not carried (8s on 200s exceeds the 3%% tolerance)", r.MBID)
	}
	if r.ISRC != "ISRC-OVER-TOLERANCE" {
		t.Errorf("ISRC = %v, want ISRC-OVER-TOLERANCE preserved from the canonical side", r.ISRC)
	}
}

func TestMergeInto_NameMatchNeverPastesLookalikeArtworkOverIdentity(t *testing.T) {
	identity := discoverytest.WithMBID(discoverytest.Track("No Idea", "Don Toliver", domain.ProviderMusicBrainz, nil), "mbid-dontoliver")
	lookalike := discoverytest.Track("No Idea", "Don Toliver", domain.ProviderDeezer, nil)
	lookalike.ImageURL = "https://wrong/lookalike.jpg"
	lookalike.ArtworkSource = "deezer"

	orders := map[string][][]domain.SearchResult{
		"identity_first":  {{identity}, {lookalike}},
		"lookalike_first": {{lookalike}, {identity}},
	}
	for name, order := range orders {
		entities := Merge(order)
		if len(entities) != 1 {
			t.Fatalf("%s: got %d entities, want 1 (same title+artist name-merge)", name, len(entities))
		}
		got := entities[0].Result
		if got.ImageURL != "" {
			t.Errorf("%s: ImageURL = %q, want empty placeholder (never the look-alike cover)", name, got.ImageURL)
		}
		if got.MBID != "mbid-dontoliver" {
			t.Errorf("%s: MBID = %q, want the identity preserved", name, got.MBID)
		}
	}
}

func TestMergeInto_StrongIDMergeAdoptsSiblingArtwork(t *testing.T) {
	a := discoverytest.WithISRC(discoverytest.Track("HUMBLE.", "Kendrick Lamar", domain.ProviderMusicBrainz, nil), "USUM71703089")
	b := discoverytest.WithISRC(discoverytest.Track("Humble", "Kendrick Lamar", domain.ProviderDeezer, nil), "USUM71703089")
	b.ImageURL = "https://cover/humble.jpg"
	b.ArtworkSource = "deezer"

	entities := Merge([][]domain.SearchResult{{a}, {b}})
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
		discoverytest.Track("Filler One", "X", domain.ProviderDeezer, nil),
		discoverytest.Track("Filler Two", "X", domain.ProviderDeezer, nil),
		discoverytest.Track("Target", "Kendrick Lamar", domain.ProviderDeezer, map[string]any{"isrc": "ISRC1"}),
	}
	groupB := []domain.SearchResult{
		discoverytest.Track("Target", "Kendrick Lamar", domain.ProviderITunes, map[string]any{"isrc": "ISRC1"}),
	}
	entities := Merge([][]domain.SearchResult{groupA, groupB})
	e := findByTitle(t, entities, "Target")
	if e.BestRank[domain.ProviderDeezer] != 2 {
		t.Errorf("deezer best rank = %d, want 2", e.BestRank[domain.ProviderDeezer])
	}
	if e.BestRank[domain.ProviderITunes] != 0 {
		t.Errorf("itunes best rank = %d, want 0", e.BestRank[domain.ProviderITunes])
	}
}

func TestMerge_BridgeTierMergesCrossProvider(t *testing.T) {
	mb := discoverytest.WithMBID(discoverytest.Res(domain.ResultKindTrack, "Bridge Recording One", "Artist X", domain.ProviderMusicBrainz, nil), "mbid-1")
	mb.Xref = map[domain.ProviderKey]string{"deezer": "555"}
	dz := domain.SearchResult{
		Kind:     domain.ResultKindTrack,
		Title:    "Totally Different Title",
		Subtitle: "Artist X",
		Sources:  []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "555", URL: "https://deezer/555"}},
		Extras:   map[string]any{},
	}

	entities := Merge([][]domain.SearchResult{{mb}, {dz}})

	if len(entities) != 1 {
		t.Fatalf("bridge merge failed: got %d entities, want 1 (bridge did not fire)", len(entities))
	}
	if tier := entities[0].Result.ResolutionTier.Tier.String(); tier != domain.EntityResolutionBridge.String() {
		t.Fatalf("resolution tier = %v, want %q", tier, domain.EntityResolutionBridge.String())
	}
	if entities[0].Result.Confidence != domain.ConfidenceHigh {
		t.Fatalf("bridge merge should be high confidence, got %v", entities[0].Result.Confidence)
	}
}

func TestMerge_NoBridgeWithoutXref(t *testing.T) {
	a := discoverytest.WithMBID(discoverytest.Res(domain.ResultKindTrack, "Distinct One", "Artist X", domain.ProviderMusicBrainz, nil), "mbid-1")
	b := domain.SearchResult{
		Kind:     domain.ResultKindTrack,
		Title:    "Distinct Two",
		Subtitle: "Artist X",
		Sources:  []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "555", URL: "https://deezer/555"}},
		Extras:   map[string]any{},
	}
	entities := Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 2 {
		t.Fatalf("without an xref these distinct-title results must not merge: got %d entities, want 2", len(entities))
	}
}

func TestMerge_TitleTierNeverPairsTheOriginalISRCWithTheRemixMBIDInEitherOrder(t *testing.T) {
	original := discoverytest.WithISRC(discoverytest.Track("Drinking in L.A.", "Bran Van 3000", domain.ProviderDeezer, nil), "CAA509814003")
	original.Duration = 236
	remix := discoverytest.WithMBID(discoverytest.Track("Drinking in L.A. (Who Mix?)", "Bran Van 3000", domain.ProviderMusicBrainz, nil), "d3dc2825")
	remix.Duration = 307

	orders := map[string][][]domain.SearchResult{
		"original_first": {{original}, {remix}},
		"remix_first":    {{remix}, {original}},
	}
	for name, order := range orders {
		entities := Merge(order)
		if len(entities) != 1 {
			t.Fatalf("%s: got %d entities, want 1 (title tier still merges)", name, len(entities))
		}
		r := entities[0].Result
		if r.MBID == "d3dc2825" && r.ISRC == "CAA509814003" {
			t.Errorf("%s: merged entity pairs the original's ISRC CAA509814003 with the remix's MBID d3dc2825", name)
		}
		if got := len(r.Sources); got != 2 {
			t.Errorf("%s: sources = %d, want 2 (both providers still union into one row)", name, got)
		}
	}
}

func TestMerge_TitleTierNeverPairsTheOriginalMBIDWithARemixISRCInEitherOrder(t *testing.T) {
	original := discoverytest.WithMBID(discoverytest.Track("Song Title", "Some Artist", domain.ProviderMusicBrainz, nil), "mbid-original")
	original.Duration = 200
	remix := discoverytest.WithISRC(discoverytest.Track("Song Title (Club Remix)", "Some Artist", domain.ProviderDeezer, nil), "ISRC-REMIX")
	remix.Duration = 320

	orders := map[string][][]domain.SearchResult{
		"original_first": {{original}, {remix}},
		"remix_first":    {{remix}, {original}},
	}
	for name, order := range orders {
		entities := Merge(order)
		if len(entities) != 1 {
			t.Fatalf("%s: got %d entities, want 1 (title tier still merges)", name, len(entities))
		}
		r := entities[0].Result
		if r.MBID == "mbid-original" && r.ISRC == "ISRC-REMIX" {
			t.Errorf("%s: merged entity pairs the original's MBID with the remix's ISRC", name)
		}
	}
}

func TestMerge_TitleTierDoesNotCarryMBIDAcrossAQualifierEvenWhenDurationsMatch(t *testing.T) {
	studio := discoverytest.WithISRC(discoverytest.Track("Fix You", "Coldplay", domain.ProviderDeezer, nil), "GBAYE0500000")
	studio.Duration = 295
	live := discoverytest.WithMBID(discoverytest.Track("Fix You (Live)", "Coldplay", domain.ProviderMusicBrainz, nil), "mbid-live")
	live.Duration = 295

	entities := Merge([][]domain.SearchResult{{studio}, {live}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1 (title tier still merges)", len(entities))
	}
	r := entities[0].Result
	if r.MBID == "mbid-live" {
		t.Errorf("MBID = %q, want the live recording's MBID not carried (titles differ under identity normalisation)", r.MBID)
	}
	if r.ISRC != "GBAYE0500000" {
		t.Errorf("ISRC = %q, want GBAYE0500000 kept", r.ISRC)
	}
}

func TestMerge_TitleTierDoesNotCarryJustAboveTheFiveSecondFloor(t *testing.T) {
	a := discoverytest.WithISRC(discoverytest.Track("Song Title", "Some Artist", domain.ProviderDeezer, nil), "ISRC-ABOVE-FLOOR")
	a.Duration = 100
	b := discoverytest.WithMBID(discoverytest.Track("Song Title", "Some Artist", domain.ProviderMusicBrainz, nil), "MBID-ABOVE-FLOOR")
	b.Duration = 106

	entities := Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1 (title tier still merges)", len(entities))
	}
	r := entities[0].Result
	if r.MBID == "MBID-ABOVE-FLOOR" {
		t.Errorf("MBID = %q, want it not carried (6s on 100s exceeds max(5s, 3s))", r.MBID)
	}
	if r.ISRC != "ISRC-ABOVE-FLOOR" {
		t.Errorf("ISRC = %q, want ISRC-ABOVE-FLOOR kept from the canonical side", r.ISRC)
	}
}

func TestMerge_TitleTierCarriesTheAlbumMBIDPastARejectedRemix(t *testing.T) {
	deezer := discoverytest.WithISRC(discoverytest.Track("Drinking in L.A.", "Bran Van 3000", domain.ProviderDeezer, nil), "CAA509814003")
	deezer.Duration = 236
	remix := discoverytest.WithMBID(discoverytest.Track("Drinking in L.A. (Who Mix?)", "Bran Van 3000", domain.ProviderMusicBrainz, nil), "d3dc2825")
	remix.Duration = 307
	album := discoverytest.WithMBID(discoverytest.Track("Drinking in L.A.", "Bran Van 3000", domain.ProviderMusicBrainz, nil), "5d6efd30")
	album.Duration = 236

	entities := Merge([][]domain.SearchResult{{deezer}, {remix, album}})
	var got Entity
	for _, e := range entities {
		if e.Result.ISRC == "CAA509814003" {
			got = e
		}
	}
	if got.Result.ISRC == "" {
		t.Fatalf("no entity carries ISRC CAA509814003 among %d entities", len(entities))
	}
	if got.Result.MBID != "5d6efd30" {
		t.Errorf("MBID = %q, want 5d6efd30 (the album recording's MBID, never the remix's d3dc2825)", got.Result.MBID)
	}
}

func TestMerge_ISRCTierStillCarriesMBIDAcrossDifferentTitlesAndDurations(t *testing.T) {
	a := discoverytest.WithISRC(discoverytest.Track("Song Title", "Some Artist", domain.ProviderDeezer, nil), "ISRC-SHARED")
	a.Duration = 200
	b := discoverytest.WithMBID(discoverytest.WithISRC(discoverytest.Track("Song Title (2011 Remaster)", "Some Artist", domain.ProviderMusicBrainz, nil), "ISRC-SHARED"), "mbid-shared")
	b.Duration = 260

	entities := Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1 (same ISRC)", len(entities))
	}
	r := entities[0].Result
	if tier := r.ResolutionTier.Tier.String(); tier != "isrc" {
		t.Errorf("resolution_tier = %v, want isrc", tier)
	}
	if r.MBID != "mbid-shared" {
		t.Errorf("MBID = %q, want mbid-shared carried (stronger tiers are unchanged)", r.MBID)
	}
}

func TestMerge_ArtistNameTierCarriesMBIDAcrossAParenthetical(t *testing.T) {
	poison := discoverytest.Res(domain.ResultKindArtist, "Poison", "", domain.ProviderDeezer, nil)
	poisonBand := discoverytest.WithMBID(discoverytest.Res(domain.ResultKindArtist, "Poison (Band)", "", domain.ProviderMusicBrainz, nil), "mbid-poison")

	entities := Merge([][]domain.SearchResult{{poison}, {poisonBand}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1 (artist name tier still merges)", len(entities))
	}
	r := entities[0].Result
	if r.MBID != "mbid-poison" {
		t.Errorf("MBID = %q, want mbid-poison (the identity-agreement gate must not apply to the artist branch)", r.MBID)
	}
}

func TestMerge_TitleTierKeepsProvenIdentityOverALaterMoreCompleteCandidate(t *testing.T) {
	deezer := discoverytest.WithISRC(discoverytest.Track("Song Title", "Some Artist", domain.ProviderDeezer, nil), "ISRC-PROVEN")
	mb := discoverytest.WithMBID(discoverytest.WithISRC(discoverytest.Track("Song Title", "Some Artist", domain.ProviderMusicBrainz, nil), "ISRC-PROVEN"), "mbid-proven")
	extended := discoverytest.Track("Song Title (Extended)", "Some Artist", domain.ProviderLastFM, nil)
	extended.Album = "Some Album"
	extended.ImageURL = "https://x/art.jpg"
	extended.UPC = "000000000000"
	extended.Duration = 400

	entities := Merge([][]domain.SearchResult{{deezer}, {mb}, {extended}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1 (title tier still merges)", len(entities))
	}
	r := entities[0].Result
	if r.ISRC != "ISRC-PROVEN" {
		t.Errorf("ISRC = %q, want ISRC-PROVEN kept (the proven side, not the merely more complete candidate)", r.ISRC)
	}
	if r.MBID != "mbid-proven" {
		t.Errorf("MBID = %q, want mbid-proven kept (the proven side, not the merely more complete candidate)", r.MBID)
	}
}

func TestMerge_DifferentISRCsKeepARemixApartFromTheOriginal(t *testing.T) {
	original := discoverytest.WithISRC(discoverytest.Track("Song", "X", domain.ProviderDeezer, nil), "USUM71700001")
	remix := discoverytest.WithISRC(discoverytest.Track("Song (Remix)", "X", domain.ProviderSpotify, nil), "USUM71700002")

	entities := Merge([][]domain.SearchResult{{original}, {remix}})

	if len(entities) != 2 {
		t.Fatalf("got %d entities, want the remix and the original kept apart", len(entities))
	}
}

func TestMerge_DifferentISRCsStillMergeWhenTheVersionWordsAgree(t *testing.T) {
	a := discoverytest.WithISRC(discoverytest.Track("Song", "X", domain.ProviderDeezer, nil), "USUM71700001")
	b := discoverytest.WithISRC(discoverytest.Track("Song", "X", domain.ProviderSpotify, nil), "USUM71700002")

	entities := Merge([][]domain.SearchResult{{a}, {b}})

	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(entities))
	}
}

func TestMerge_ExplicitAndCleanTwinsCarryTheISRCOfTheSameSource(t *testing.T) {
	explicit := discoverytest.WithISRC(discoverytest.Track("Song", "X", domain.ProviderDeezer, map[string]any{domain.ExtraExplicit: true}), "USUM71700001")
	clean := discoverytest.WithISRC(discoverytest.Track("Song", "X", domain.ProviderSpotify, nil), "USUM71700002")
	clean.Album = "Clean Album"

	for _, order := range [][][]domain.SearchResult{{{explicit}, {clean}}, {{clean}, {explicit}}} {
		entities := Merge(order)
		if len(entities) != 1 {
			t.Fatalf("got %d entities, want the twins merged", len(entities))
		}
		r := entities[0].Result
		if got, want := domain.BoolExtra(r.Extras, domain.ExtraExplicit), r.ISRC == "USUM71700001"; got != want {
			t.Errorf("ISRC %q with explicit=%v: the flag must come from the ISRC's own source", r.ISRC, got)
		}
	}
}

func TestMerge_ISRCsDifferingOnlyInFormattingMatch(t *testing.T) {
	soundcloud := discoverytest.WithISRC(discoverytest.Track("Song", "X", domain.ProviderSoundCloud, nil), "us-um7-17-03861")
	deezer := discoverytest.WithISRC(discoverytest.Track("Song (Remix)", "Y", domain.ProviderDeezer, nil), "USUM71703861")

	entities := Merge([][]domain.SearchResult{{soundcloud}, {deezer}})

	if len(entities) != 1 {
		t.Fatalf("got %d entities, want the dashed and canonical ISRC to match", len(entities))
	}
}

func artistWithID(name string, provider domain.ProviderName, id string, fans int64, image string) domain.SearchResult {
	return domain.SearchResult{
		Kind:     domain.ResultKindArtist,
		Title:    name,
		ImageURL: image,
		FanCount: fans,
		Sources:  []domain.SourceRef{{Provider: provider, ExternalID: id}},
	}
}

func entityWithSource(t *testing.T, entities []Entity, provider domain.ProviderName, id string) Entity {
	t.Helper()
	for _, e := range entities {
		for _, s := range e.Result.Sources {
			if s.Provider == provider && s.ExternalID == id {
				return e
			}
		}
	}
	t.Fatalf("no entity holds %s:%s", provider, id)
	return Entity{}
}

func TestMerge_SameNameArtistsWithDifferentIDsFromOneProviderStayApart(t *testing.T) {
	entities := Merge([][]domain.SearchResult{{
		artistWithID("Michael Jackson", domain.ProviderDeezer, "373427811", 211, "https://img/fan.jpg"),
		artistWithID("Michael Jackson", domain.ProviderDeezer, "259", 13187101, "https://img/mj.jpg"),
	}})

	if len(entities) != 2 {
		t.Fatalf("got %d entities, want 2: two Deezer ids are two different artists", len(entities))
	}
}

func TestMerge_AnotherProviderJoinsTheMostPopularSameNameArtist(t *testing.T) {
	entities := Merge([][]domain.SearchResult{
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
	entities := Merge([][]domain.SearchResult{
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
	first := discoverytest.Res(domain.ResultKindTrack, "Smooth Criminal", "Michael Jackson", domain.ProviderSoundCloud, nil)
	second := discoverytest.Res(domain.ResultKindTrack, "Smooth Criminal", "Michael Jackson", domain.ProviderSoundCloud, nil)
	second.Sources[0].ExternalID = "another-upload"

	if entities := Merge([][]domain.SearchResult{{first, second}}); len(entities) != 1 {
		t.Fatalf("got %d entities, want 1: the same-provider rule is for artists only", len(entities))
	}
}

func TestMerge_TheMergedArtistKeepsSpotifysTopPick(t *testing.T) {
	picked := artistWithID("Cochise", domain.ProviderSpotify, "sp1", 0, "https://img/sp.jpg")
	picked.Extras = map[string]any{domain.ExtraTopPick: true}

	entities := Merge([][]domain.SearchResult{
		{artistWithID("Cochise", domain.ProviderDeezer, "dz1", 500000, "https://img/dz.jpg")},
		{picked},
	})

	merged := entityWithSource(t, entities, domain.ProviderDeezer, "dz1")
	if len(merged.Result.Sources) != 2 || !domain.BoolExtra(merged.Result.Extras, domain.ExtraTopPick) {
		t.Fatalf("merged artist = sources %v extras %v, want Spotify joined and the top pick kept", merged.Result.Sources, merged.Result.Extras)
	}
}

func TestMerge_ASongCreditedWithExtraNamesFoldsIntoTheLeadArtistsSong(t *testing.T) {
	entities := Merge([][]domain.SearchResult{
		{discoverytest.Track("Yacht Club (feat. Juice WRLD)", "Lil Yachty", domain.ProviderDeezer, nil)},
		{discoverytest.Track("Yacht Club (feat. Juice WRLD)", "Lil Yachty, RD, Lil Boat", domain.ProviderSoundCloud, nil)},
	})

	if len(entities) != 1 {
		t.Fatalf("got %d entities, want one Yacht Club", len(entities))
	}
}

func TestMerge_AnAlbumWithACoCreditFoldsIntoTheLeadArtistsAlbum(t *testing.T) {
	entities := Merge([][]domain.SearchResult{
		{discoverytest.Res(domain.ResultKindAlbum, "Her Loss", "Drake & 21 Savage", domain.ProviderAppleMusic, nil)},
		{discoverytest.Res(domain.ResultKindAlbum, "Her Loss", "Drake", domain.ProviderSpotify, nil)},
	})

	if len(entities) != 1 {
		t.Fatalf("got %d entities, want one Her Loss", len(entities))
	}
}

func TestMerge_SameTitleByADifferentLeadArtistStaysApart(t *testing.T) {
	entities := Merge([][]domain.SearchResult{
		{discoverytest.Track("Cochise", "Audioslave", domain.ProviderDeezer, nil)},
		{discoverytest.Track("Cochise", "Cochise, Audioslave", domain.ProviderSoundCloud, nil)},
	})

	if len(entities) != 2 {
		t.Fatalf("got %d entities, want the two songs apart", len(entities))
	}
}

func TestMerge_TheCatalogCreditBeatsASoundCloudAccountName(t *testing.T) {
	upload := discoverytest.Track("Yacht Club (feat. Juice WRLD)", "Lil Yachty, RD, Lil Boat", domain.ProviderSoundCloud, nil)
	upload.ImageURL, upload.Duration, upload.Album = "https://img/sc.jpg", 180, "Lil Boat"

	for _, order := range [][][]domain.SearchResult{
		{{discoverytest.Track("Yacht Club (feat. Juice WRLD)", "Lil Yachty", domain.ProviderDeezer, nil)}, {upload}},
		{{upload}, {discoverytest.Track("Yacht Club (feat. Juice WRLD)", "Lil Yachty", domain.ProviderDeezer, nil)}},
	} {
		entities := Merge(order)
		if len(entities) != 1 {
			t.Fatalf("got %d entities, want one Yacht Club", len(entities))
		}
		if got := entities[0].Result.Subtitle; got != "Lil Yachty" {
			t.Errorf("subtitle %q, want the catalog credit Lil Yachty", got)
		}
	}
}

func TestMerge_SymbolOnlyArtistNamesStillMatch(t *testing.T) {
	entities := Merge([][]domain.SearchResult{
		{artistWithID("¥$", domain.ProviderDeezer, "dz", 900000, "https://img/dz.jpg")},
		{artistWithID("¥$", domain.ProviderSpotify, "sp", 0, "https://img/sp.jpg")},
	})

	if len(entities) != 1 {
		t.Fatalf("got %d entities, want the Deezer and Spotify ¥$ as one", len(entities))
	}
}

func TestMerge_DifferentSymbolOnlyNamesStayApart(t *testing.T) {
	entities := Merge([][]domain.SearchResult{
		{artistWithID("¥$", domain.ProviderDeezer, "dz", 900000, "")},
		{artistWithID("$$$", domain.ProviderSpotify, "sp", 0, "")},
	})

	if len(entities) != 2 {
		t.Fatalf("got %d entities, want ¥$ and $$$ apart", len(entities))
	}
}

func TestMerge_AnotherProviderJoinsTheMostPopularSameNameArtistWhicheverComesFirst(t *testing.T) {
	entities := Merge([][]domain.SearchResult{
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
