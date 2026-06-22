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
		Extras: extras,
	}
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

func TestMerge_IdentifierMatch(t *testing.T) {
	t.Run("same isrc merges across providers", func(t *testing.T) {
		a := track("HUMBLE.", "Kendrick Lamar", domain.ProviderDeezer, map[string]any{"isrc": "USUM71703089"})
		b := track("Humble", "Kendrick Lamar", domain.ProviderITunes, map[string]any{"isrc": "USUM71703089"})
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
		a := track("Intro", "Artist A", domain.ProviderMusicBrainz, map[string]any{"mbid": "mbid-1"})
		b := track("Intro", "Artist A", domain.ProviderMusicBrainz, map[string]any{"mbid": "mbid-2"})
		entities := Merge([][]domain.SearchResult{{a, b}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 (distinct MBIDs)", len(entities))
		}
	})
}

func TestMerge_IdentityBridge(t *testing.T) {
	// MB artist "Ye" carries an mbid and a bridged Deezer id (stamped pre-merge
	// from the IdentityBridge via extras["xref"]). The Deezer result "Kanye West"
	// has that exact native id. The titles differ, so only the stated id proves
	// they are the same entity — name similarity never would.
	mb := domain.SearchResult{
		Kind:    domain.ResultKindArtist,
		Title:   "Ye",
		Sources: []domain.SourceRef{{Provider: domain.ProviderMusicBrainz, ExternalID: "mbid-ye"}},
		Extras: map[string]any{
			"mbid": "mbid-ye",
			"xref": map[string]string{"deezer": "230"},
		},
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
		bare.Extras = map[string]any{"mbid": "mbid-ye"} // no xref
		entities := Merge([][]domain.SearchResult{{bare}, {dz}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 — no stated id, differing titles must not merge", len(entities))
		}
	})

	t.Run("a non-matching stated id does not merge", func(t *testing.T) {
		wrong := mb
		wrong.Extras = map[string]any{"mbid": "mbid-ye", "xref": map[string]string{"deezer": "999"}}
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
