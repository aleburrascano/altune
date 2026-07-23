package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// res builds a SearchResult with one source for the given provider.
func res(kind domain.ResultKind, title, subtitle string, provider domain.ProviderName, extras map[string]any) domain.SearchResult {
	return domain.SearchResult{
		Kind:     kind,
		Title:    title,
		Subtitle: subtitle,
		Sources: []domain.SourceRef{
			{Provider: provider, ExternalID: title + ":" + provider.String(), URL: "https://x/" + title},
		},
		Popularity: popFromExtras(extras),
		Extras:     extras,
	}
}

// popFromExtras lifts a fixture's legacy "popularity" key into the typed
// Popularity field, mirroring how providers populate it at ACL translation.
func popFromExtras(extras map[string]any) float64 {
	switch n := extras["popularity"].(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	}
	return 0
}

func track(title, artist string, provider domain.ProviderName, extras map[string]any) domain.SearchResult {
	return res(domain.ResultKindTrack, title, artist, provider, extras)
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
	// MB returns two distinct "Che" artists (different MBIDs) → the name is
	// ambiguous. A no-identifier provider artist of the same name must NOT be
	// absorbed by name alone — every entity keeps only its own source.
	mb1 := withMBID(res(domain.ResultKindArtist, "Che", "", domain.ProviderMusicBrainz, nil), "mbid-1")
	mb2 := withMBID(res(domain.ResultKindArtist, "Che", "", domain.ProviderMusicBrainz, nil), "mbid-2")
	itunes := res(domain.ResultKindArtist, "Che", "", domain.ProviderITunes, nil)

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
	// One MB identity for the name → unambiguous → a no-identifier provider artist
	// of the same name merges by name as before (no regression for e.g. Drake).
	mb := withMBID(res(domain.ResultKindArtist, "Drake", "", domain.ProviderMusicBrainz, nil), "mbid-drake")
	itunes := res(domain.ResultKindArtist, "Drake", "", domain.ProviderITunes, nil)

	entities := Merge([][]domain.SearchResult{{mb}, {itunes}})

	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1 (name merge preserved when unambiguous)", len(entities))
	}
	if got := len(entities[0].Result.Sources); got != 2 {
		t.Errorf("merged entity has %d sources, want 2", got)
	}
}

func TestMerge_ITunesBridgesIntoMBIdentityDespiteAmbiguousName(t *testing.T) {
	// MB "Che" is stamped with its Apple Music id (Xref["itunes"]); the iTunes
	// "Che" carries that same artistId natively. They bridge and merge even though
	// the name is ambiguous (a second distinct MB "Che" exists) — identity beats
	// the name-ambiguity gate.
	mb := domain.SearchResult{
		Kind:    domain.ResultKindArtist,
		Title:   "Che",
		Sources: []domain.SourceRef{{Provider: domain.ProviderMusicBrainz, ExternalID: "mbid-che-1", URL: "https://mb/1"}},
		MBID:    "mbid-che-1",
		Xref:    map[string]string{"itunes": "5468295"},
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
	bridged := findByTitle(t, entities, "Che") // first "Che" = MB#1 (the xref carrier)
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
		a := withISRC(track("HUMBLE.", "Kendrick Lamar", domain.ProviderDeezer, nil), "USUM71703089")
		b := withISRC(track("Humble", "Kendrick Lamar", domain.ProviderITunes, nil), "USUM71703089")
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 1 {
			t.Fatalf("got %d entities, want 1", len(entities))
		}
		if got := len(entities[0].Result.Sources); got != 2 {
			t.Errorf("sources = %d, want 2 (unioned)", got)
		}
		if tier := entities[0].Result.Extras["resolution_tier"]; tier != "isrc" {
			t.Errorf("resolution_tier = %v, want isrc", tier)
		}
		if entities[0].Result.Confidence != domain.ConfidenceHigh {
			t.Errorf("confidence = %v, want high", entities[0].Result.Confidence)
		}
	})

	t.Run("distinct mbid stays separate", func(t *testing.T) {
		a := withMBID(track("Intro", "Artist A", domain.ProviderMusicBrainz, nil), "mbid-1")
		b := withMBID(track("Intro", "Artist A", domain.ProviderMusicBrainz, nil), "mbid-2")
		entities := Merge([][]domain.SearchResult{{a, b}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 (distinct MBIDs)", len(entities))
		}
	})
}

func TestMerge_AlbumUPCTier(t *testing.T) {
	t.Run("same upc merges albums across providers", func(t *testing.T) {
		a := res(domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", domain.ProviderAppleMusic, nil)
		a.UPC = "00602557618280"
		b := res(domain.ResultKindAlbum, "DAMN. (Deluxe)", "Kendrick Lamar", domain.ProviderDeezer, nil)
		b.UPC = "00602557618280"
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 1 {
			t.Fatalf("got %d entities, want 1", len(entities))
		}
		if tier := entities[0].Result.Extras["resolution_tier"]; tier != "upc" {
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
		a := res(domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", domain.ProviderAppleMusic, nil)
		a.UPC = "00602557618280"
		b := res(domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", domain.ProviderDeezer, nil)
		b.UPC = "00602557618297" // a different edition's barcode is not disproof
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 1 {
			t.Fatalf("got %d entities, want 1 (upc mismatch must not block)", len(entities))
		}
	})

	t.Run("upc never merges tracks", func(t *testing.T) {
		a := track("Same Barcode", "Artist A", domain.ProviderDeezer, nil)
		a.UPC = "00602557618280"
		b := track("Different Title", "Artist B", domain.ProviderITunes, nil)
		b.UPC = "00602557618280"
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 (upc tier is album-only)", len(entities))
		}
	})
}

func TestMergeInto_CoalescesTypedContentFields(t *testing.T) {
	a := track("HUMBLE.", "Kendrick Lamar", domain.ProviderDeezer, nil)
	a.Album = "DAMN."
	a.Duration = 177
	a.DeezerAlbumID = "13114014"
	b := track("HUMBLE.", "Kendrick Lamar", domain.ProviderITunes, nil)
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
	// MB artist "Ye" carries an mbid and a bridged Deezer id (stamped pre-merge
	// from the IdentityBridge onto Xref). The Deezer result "Kanye West"
	// has that exact native id. The titles differ, so only the stated id proves
	// they are the same entity — name similarity never would.
	mb := domain.SearchResult{
		Kind:    domain.ResultKindArtist,
		Title:   "Ye",
		Sources: []domain.SourceRef{{Provider: domain.ProviderMusicBrainz, ExternalID: "mbid-ye"}},
		MBID:    "mbid-ye",
		Xref:    map[string]string{"deezer": "230"},
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
		if tier := entities[0].Result.Extras["resolution_tier"]; tier != "bridge" {
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
		bare.Xref = nil // no xref
		entities := Merge([][]domain.SearchResult{{bare}, {dz}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 — no stated id, differing titles must not merge", len(entities))
		}
	})

	t.Run("a non-matching stated id does not merge", func(t *testing.T) {
		wrong := mb
		wrong.Xref = map[string]string{"deezer": "999"}
		entities := Merge([][]domain.SearchResult{{wrong}, {dz}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 — a mismatched stated id must not merge", len(entities))
		}
	})
}

func TestMerge_SequelStaysSeparate(t *testing.T) {
	// Pattern B: a trailing sequel number survives canonical normalization, so
	// the sequel never collapses into the original — with no version machinery.
	a := track("Shotta Flow", "NLE Choppa", domain.ProviderDeezer, map[string]any{"popularity": 90.0})
	b := track("Shotta Flow 2", "NLE Choppa", domain.ProviderDeezer, map[string]any{"popularity": 40.0})
	entities := Merge([][]domain.SearchResult{{a, b}})
	if len(entities) != 2 {
		t.Fatalf("got %d entities, want 2 — the sequel must not collapse into the original", len(entities))
	}
}

func TestMerge_ParentheticalVariantsCollapse(t *testing.T) {
	// Parenthetical markers are canonical noise (textnorm strips them), so a
	// remix/live/feat variant folds into the base title — intentionally NOT a
	// separate entity (the over-merging that broke re-find is gone the other way:
	// we no longer fold a dash-form variant into a paren-form one; see below).
	cases := []struct{ a, b string }{
		{"Bad", "Bad (Remix)"},
		{"Fix You", "Fix You (Live)"},
		{"Goosebumps", "Goosebumps (feat. Kevin Abstract)"},
	}
	for _, tc := range cases {
		a := track(tc.a, "Some Artist", domain.ProviderDeezer, nil)
		b := track(tc.b, "Some Artist", domain.ProviderITunes, nil)
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 1 {
			t.Errorf("%q + %q: got %d entities, want 1 (parenthetical variant folds in)", tc.a, tc.b, len(entities))
		}
	}
}

func TestMerge_DashAndParenVariantsStaySeparate(t *testing.T) {
	// The regression fix: a dash-suffixed variant keeps its suffix tokens after
	// normalization ("big poppa 2005 remaster") and so does NOT merge into a
	// paren-form entity ("big poppa") — the exact saved variant survives.
	a := track("Big Poppa - 2005 Remaster", "The Notorious B.I.G.", domain.ProviderLastFM, nil)
	b := track("Big Poppa (2007 Remaster)", "The Notorious B.I.G.", domain.ProviderDeezer, nil)
	entities := Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 2 {
		t.Fatalf("got %d entities, want 2 — the dash-form variant must stay distinct", len(entities))
	}
}

func TestMerge_SameTitleAcrossProvidersMerges(t *testing.T) {
	a := track("HUMBLE.", "Kendrick Lamar", domain.ProviderDeezer, nil)
	b := track("Humble", "Kendrick Lamar", domain.ProviderITunes, nil)
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
	// No fuzzy rung anymore: a typo'd title is a different canonical string, so
	// it is a separate entity. The duplicate is the accepted cost of dropping a
	// tuned threshold; ranking surfaces both.
	a := track("Bohemian Rhapsody", "Queen", domain.ProviderDeezer, nil)
	b := track("Bohemian Rapsody", "Queen", domain.ProviderITunes, nil)
	entities := Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 2 {
		t.Fatalf("got %d entities, want 2 (no fuzzy merge)", len(entities))
	}
}

func TestMerge_DifferentArtistStaysSeparate(t *testing.T) {
	a := track("Crazy", "Gnarls Barkley", domain.ProviderDeezer, nil)
	b := track("Crazy", "Patsy Cline", domain.ProviderITunes, nil)
	entities := Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 2 {
		t.Fatalf("got %d entities, want 2 (same title, different artists)", len(entities))
	}
}

func TestMerge_Artists(t *testing.T) {
	t.Run("same-name artists merge and union sources", func(t *testing.T) {
		a := res(domain.ResultKindArtist, "Drake", "", domain.ProviderDeezer, nil)
		b := res(domain.ResultKindArtist, "Drake", "", domain.ProviderITunes, nil)
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 1 {
			t.Fatalf("got %d entities, want 1", len(entities))
		}
		if got := len(entities[0].Result.Sources); got != 2 {
			t.Errorf("sources = %d, want 2", got)
		}
	})

	t.Run("distinct names stay separate", func(t *testing.T) {
		a := res(domain.ResultKindArtist, "Blink-182", "", domain.ProviderDeezer, nil)
		b := res(domain.ResultKindArtist, "Blink", "", domain.ProviderITunes, nil)
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 (Blink-182 != Blink)", len(entities))
		}
	})
}

func TestAmbiguousArtistNames_OnlyMusicBrainzMBIDsCount(t *testing.T) {
	// A stale Last.fm mbid for the same artist must not register as a second
	// "identity": the set's semantics are "names for which MUSICBRAINZ surfaced
	// ≥2 MBIDs", and inflating it refuses legitimate bare-name merges (duplicate
	// artist cards).
	mb := withMBID(res(domain.ResultKindArtist, "Queen", "", domain.ProviderMusicBrainz, nil), "mbid-current")
	lastfm := withMBID(res(domain.ResultKindArtist, "Queen", "", domain.ProviderLastFM, nil), "mbid-stale")

	if got := ambiguousArtistNamesFlat([]domain.SearchResult{mb, lastfm}); got["queen"] {
		t.Error("MB + stale Last.fm mbid marked the name ambiguous — only MusicBrainz MBIDs may count")
	}

	mb2 := withMBID(res(domain.ResultKindArtist, "Queen", "", domain.ProviderMusicBrainz, nil), "mbid-second")
	if got := ambiguousArtistNamesFlat([]domain.SearchResult{mb, mb2}); !got["queen"] {
		t.Error("two genuine MB identities with distinct MBIDs must still mark the name ambiguous")
	}
}

func TestMerge_UPCTierRefusesConflictingMBIDs(t *testing.T) {
	// A(m1,upc) and B(m2,upc) must NOT merge on UPC: mergeInto keeps only one
	// MBID, so a later C(m2) would hard-stop against the survivor and the entity
	// count would depend on arrival order. With conflicting MBIDs falling through
	// to the hard-stop, the grouping is order-independent: {A} and {B+C}.
	mkAlbum := func(title, mbid string, provider domain.ProviderName) domain.SearchResult {
		r := withMBID(res(domain.ResultKindAlbum, title, "Kendrick Lamar", provider, nil), mbid)
		r.UPC = "00602557618280"
		return r
	}
	a := mkAlbum("DAMN.", "mbid-1", domain.ProviderAppleMusic)
	b := mkAlbum("DAMN. (Deluxe)", "mbid-2", domain.ProviderDeezer)
	c := withMBID(res(domain.ResultKindAlbum, "DAMN. (Deluxe Edition)", "Kendrick Lamar", domain.ProviderMusicBrainz, nil), "mbid-2")

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
		// "(Intro)" and "(Outro)" both normalize to "" — shared emptiness is not
		// a shared title, so the text tier must refuse.
		a := track("(Intro)", "Same Artist", domain.ProviderDeezer, nil)
		b := track("(Outro)", "Same Artist", domain.ProviderITunes, nil)
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 (empty normalized titles must not merge)", len(entities))
		}
	})

	t.Run("symbol-only artist names stay separate", func(t *testing.T) {
		a := res(domain.ResultKindArtist, "!!!", "", domain.ProviderDeezer, nil)
		b := res(domain.ResultKindArtist, "†††", "", domain.ProviderITunes, nil)
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 (empty normalized names must not merge)", len(entities))
		}
	})
}

func TestMergeInto_KeepsStrongestResolutionTier(t *testing.T) {
	// An ISRC-proven entity must not be downgraded by a later name-tier merge:
	// the stamped tier and confidence keep the strongest proof seen.
	a := withISRC(track("HUMBLE.", "Kendrick Lamar", domain.ProviderDeezer, nil), "USUM71703089")
	b := withISRC(track("Humble", "Kendrick Lamar", domain.ProviderITunes, nil), "USUM71703089")
	c := track("Humble", "Kendrick Lamar", domain.ProviderLastFM, nil) // no identifier — name tier

	entities := Merge([][]domain.SearchResult{{a}, {b}, {c}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(entities))
	}
	r := entities[0].Result
	if tier := r.Extras["resolution_tier"]; tier != "isrc" {
		t.Errorf("resolution_tier = %v, want isrc (name merge must not downgrade)", tier)
	}
	if r.Confidence != domain.ConfidenceHigh {
		t.Errorf("confidence = %v, want high (identity-proven)", r.Confidence)
	}
}

func TestMerge_BestRankTracksMinAcrossProviders(t *testing.T) {
	groupA := []domain.SearchResult{
		track("Filler One", "X", domain.ProviderDeezer, nil),
		track("Filler Two", "X", domain.ProviderDeezer, nil),
		track("Target", "Kendrick Lamar", domain.ProviderDeezer, map[string]any{"isrc": "ISRC1"}),
	}
	groupB := []domain.SearchResult{
		track("Target", "Kendrick Lamar", domain.ProviderITunes, map[string]any{"isrc": "ISRC1"}),
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
