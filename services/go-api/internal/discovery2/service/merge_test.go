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

// findByTitle returns the first entity whose title matches, or fails.
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

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		wantCore string
		wantTags string
	}{
		{"plain", "Humble", "humble", ""},
		{"trailing punctuation normalizes", "HUMBLE.", "humble", ""},
		{"sequel number", "Shotta Flow 2", "shotta flow", "n:2"},
		{"part roman drops leading article", "The Saga Part II", "saga", "n:2"},
		{"pt dot arabic", "Story Pt. 3", "story", "n:3"},
		{"remix paren", "Bad (Remix)", "bad", "remix"},
		{"live bracket", "Wish You Were Here [Live]", "wish you were here", "live"},
		{"deluxe", "Scorpion (Deluxe)", "scorpion", "deluxe"},
		{"dash remaster", "Dreams - Remastered 2004", "dreams", "remaster"},
		{"feat paren", "Whats Poppin (feat. Tyga)", "whats poppin", "feat:tyga"},
		{"feat dot", "Sicko Mode feat. Drake", "sicko mode", "feat:drake"},
		// An unrecognized bracket emits no tag; normalization then drops it from
		// the core. Both sides normalize identically, so this never mis-merges.
		{"unrecognized bracket yields no tag", "(I Can't Get No) Satisfaction", "satisfaction", ""},
		{"feat plus remix sorted", "Song (feat. A) (Remix)", "song", "feat:a|remix"},
		{"bare 1 is not a sequel", "Track 1", "track 1", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseVersion(tt.title)
			if got.core != tt.wantCore {
				t.Errorf("core = %q, want %q", got.core, tt.wantCore)
			}
			if got.tags != tt.wantTags {
				t.Errorf("tags = %q, want %q", got.tags, tt.wantTags)
			}
		})
	}
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

func TestMerge_VersionMarkersKeepSequelsSeparate(t *testing.T) {
	// Pattern B: the numbered sequel must survive as its own entity.
	cases := []struct {
		name string
		a, b string
	}{
		{"sequel", "Shotta Flow", "Shotta Flow 2"},
		{"remix", "Bad", "Bad (Remix)"},
		{"feat", "Goosebumps", "Goosebumps (feat. Kevin Abstract)"},
		{"live", "Fix You", "Fix You (Live)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := track(tc.a, "Some Artist", domain.ProviderDeezer, map[string]any{"popularity": 90.0})
			b := track(tc.b, "Some Artist", domain.ProviderDeezer, map[string]any{"popularity": 40.0})
			entities := Merge([][]domain.SearchResult{{a, b}})
			if len(entities) != 2 {
				t.Fatalf("got %d entities, want 2 — %q must not collapse into %q", len(entities), tc.b, tc.a)
			}
		})
	}
}

func TestMerge_SameWorkAcrossProvidersMerges(t *testing.T) {
	// Same core, same (empty) tags, same artist, no identifiers → one work.
	a := track("HUMBLE.", "Kendrick Lamar", domain.ProviderDeezer, nil)
	b := track("Humble", "Kendrick Lamar", domain.ProviderITunes, nil)
	entities := Merge([][]domain.SearchResult{{a}, {b}})
	if len(entities) != 1 {
		t.Fatalf("got %d entities, want 1 (same work, different providers)", len(entities))
	}
	if got := len(entities[0].Result.Sources); got != 2 {
		t.Errorf("sources = %d, want 2", got)
	}
	if entities[0].Result.Confidence != domain.ConfidenceMedium {
		t.Errorf("confidence = %v, want medium (multi-source categorical merge)", entities[0].Result.Confidence)
	}
}

func TestMerge_FuzzyLastResort(t *testing.T) {
	t.Run("typo merges", func(t *testing.T) {
		a := track("Bohemian Rhapsody", "Queen", domain.ProviderDeezer, nil)
		b := track("Bohemian Rapsody", "Queen", domain.ProviderITunes, nil)
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 1 {
			t.Fatalf("got %d entities, want 1 (fuzzy typo merge)", len(entities))
		}
	})

	t.Run("different works stay separate", func(t *testing.T) {
		a := track("Yesterday", "The Beatles", domain.ProviderDeezer, nil)
		b := track("Let It Be", "The Beatles", domain.ProviderITunes, nil)
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 (distinct works)", len(entities))
		}
	})
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

	t.Run("numeric artist names are not treated as sequels", func(t *testing.T) {
		a := res(domain.ResultKindArtist, "Blink-182", "", domain.ProviderDeezer, nil)
		b := res(domain.ResultKindArtist, "Blink", "", domain.ProviderITunes, nil)
		entities := Merge([][]domain.SearchResult{{a}, {b}})
		if len(entities) != 2 {
			t.Fatalf("got %d entities, want 2 (Blink-182 != Blink)", len(entities))
		}
	})
}

func TestMerge_BestRankTracksMinAcrossProviders(t *testing.T) {
	// Same work appears at rank 2 in provider A and rank 0 in provider B.
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
