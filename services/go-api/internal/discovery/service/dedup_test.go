package service

import (
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
)

// helper: build a track result with a Deezer source (passes hasBrowseableSource for all kinds).
func trackResult(provider domain.ProviderName, extID, title, subtitle string, extras map[string]any) domain.SearchResult {
	return domain.SearchResult{
		Kind:     domain.ResultKindTrack,
		Title:    title,
		Subtitle: subtitle,
		Sources:  []domain.SourceRef{{Provider: provider, ExternalID: extID, URL: "https://example.com/" + extID}},
		Extras:   extras,
	}
}

// helper: build an artist result. Non-track results need a Deezer source to pass hasBrowseableSource.
func artistResult(provider domain.ProviderName, extID, name string, extras map[string]any) domain.SearchResult {
	return domain.SearchResult{
		Kind:     domain.ResultKindArtist,
		Title:    name,
		Sources:  []domain.SourceRef{{Provider: provider, ExternalID: extID, URL: "https://example.com/" + extID}},
		Extras:   extras,
	}
}

// helper: build an album result.
func albumResult(provider domain.ProviderName, extID, title, subtitle string, extras map[string]any) domain.SearchResult {
	return domain.SearchResult{
		Kind:     domain.ResultKindAlbum,
		Title:    title,
		Subtitle: subtitle,
		Sources:  []domain.SourceRef{{Provider: provider, ExternalID: extID, URL: "https://example.com/" + extID}},
		Extras:   extras,
	}
}

func noQualityScorer(r domain.SearchResult) domain.QualityScore {
	return domain.QualityScore{}
}

func TestFuseAndRank_MergeByISRC(t *testing.T) {
	// Two providers return the same track identified by matching ISRC.
	// Expected: merged into one result with high confidence and two sources.
	deezerTrack := trackResult(domain.ProviderDeezer, "dz-1", "Bohemian Rhapsody", "Queen", map[string]any{"isrc": "GBUM71029604"})
	mbTrack := trackResult(domain.ProviderMusicBrainz, "mb-1", "Bohemian Rhapsody", "Queen", map[string]any{"isrc": "GBUM71029604"})

	perProvider := [][]domain.SearchResult{
		{deezerTrack},
		{mbTrack},
	}

	results := FuseAndRank(perProvider, "bohemian rhapsody queen", noQualityScorer, nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 merged result, got %d", len(results))
	}
	r := results[0]
	if r.Confidence != domain.ConfidenceHigh {
		t.Errorf("expected confidence high, got %s", r.Confidence.String())
	}
	if len(r.Sources) != 2 {
		t.Errorf("expected 2 sources after merge, got %d", len(r.Sources))
	}
	tier := getStringExtra(r, "resolution_tier")
	if tier != "isrc" {
		t.Errorf("expected resolution_tier isrc, got %q", tier)
	}
}

func TestFuseAndRank_MergeByMBID(t *testing.T) {
	// Two providers return same track with matching MBID.
	// Expected: merged with high confidence, resolution_tier = mbid.
	deezerTrack := trackResult(domain.ProviderDeezer, "dz-1", "Paranoid Android", "Radiohead", map[string]any{"mbid": "abc-123"})
	mbTrack := trackResult(domain.ProviderMusicBrainz, "mb-1", "Paranoid Android", "Radiohead", map[string]any{"mbid": "abc-123"})

	perProvider := [][]domain.SearchResult{
		{deezerTrack},
		{mbTrack},
	}

	results := FuseAndRank(perProvider, "paranoid android radiohead", noQualityScorer, nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 merged result, got %d", len(results))
	}
	r := results[0]
	if r.Confidence != domain.ConfidenceHigh {
		t.Errorf("expected confidence high, got %s", r.Confidence.String())
	}
	if len(r.Sources) != 2 {
		t.Errorf("expected 2 sources after merge, got %d", len(r.Sources))
	}
	tier := getStringExtra(r, "resolution_tier")
	if tier != "mbid" {
		t.Errorf("expected resolution_tier mbid, got %q", tier)
	}
}

func TestFuseAndRank_ArtistNameNoMergeWithoutIdentifier(t *testing.T) {
	// Two providers return an artist by name without MBID — they must NOT merge.
	// Artists only merge on identifier (MBID) overlap. Name-only merge was removed
	// because it caused same-name artists (e.g., two "Che"s) to merge into one
	// result with the wrong external ID.
	// Non-track results need Deezer source to pass hasBrowseableSource — give
	// the MB artist a Deezer source too so it isn't filtered out.
	deezerArtist := artistResult(domain.ProviderDeezer, "dz-artist-1", "The Weeknd", nil)
	deezerArtist2 := artistResult(domain.ProviderDeezer, "dz-artist-2", "The Weeknd", nil)

	perProvider := [][]domain.SearchResult{
		{deezerArtist},
		{deezerArtist2},
	}

	results := FuseAndRank(perProvider, "weeknd", noQualityScorer, nil)

	if len(results) < 2 {
		t.Fatalf("expected 2 separate artist results without identifier overlap, got %d", len(results))
	}
}

func TestFuseAndRank_SameNameArtistsStaySeparate(t *testing.T) {
	// Regression test: two different artists with the same normalized name
	// from different providers, both without MBID, must remain separate.
	// This is the "Che" problem — merging them picks the wrong external ID,
	// which causes the detail screen to show the wrong artist's content.
	cheRapper := artistResult(domain.ProviderDeezer, "dz-che-rapper", "Che", map[string]any{
		"nb_fan": int64(323),
	})
	cheRock := artistResult(domain.ProviderDeezer, "dz-che-rock", "Che", map[string]any{
		"nb_fan": int64(50000),
	})

	perProvider := [][]domain.SearchResult{
		{cheRapper},
		{cheRock},
	}

	results := FuseAndRank(perProvider, "che", noQualityScorer, nil)

	if len(results) < 2 {
		t.Fatalf("expected 2 separate 'Che' artist results, got %d — same-name artists must not merge without identifier overlap", len(results))
	}
	for _, r := range results {
		if len(r.Sources) > 1 {
			t.Errorf("expected each 'Che' result to have 1 source (not merged), got %d sources", len(r.Sources))
		}
	}
}

func TestFuseAndRank_ArtistNameMergeBlockedByMBID(t *testing.T) {
	// Two artists with the same name but one has an MBID should NOT merge by name.
	// This prevents merging different artists who share a common name.
	// Both need Deezer sources to pass hasBrowseableSource for non-tracks.
	deezerArtist := artistResult(domain.ProviderDeezer, "dz-artist-1", "Megaman", nil)
	mbArtist := artistResult(domain.ProviderDeezer, "dz-artist-2", "Megaman", map[string]any{"mbid": "mb-id-123"})

	perProvider := [][]domain.SearchResult{
		{deezerArtist},
		{mbArtist},
	}

	results := FuseAndRank(perProvider, "megaman", noQualityScorer, nil)

	if len(results) < 2 {
		t.Errorf("expected 2 separate results when one has MBID, got %d", len(results))
	}
}

func TestFuseAndRank_NoMerge(t *testing.T) {
	// Two different tracks from same provider stay separate.
	track1 := trackResult(domain.ProviderDeezer, "dz-1", "Creep", "Radiohead", map[string]any{"isrc": "ISRC-AAA"})
	track2 := trackResult(domain.ProviderDeezer, "dz-2", "Karma Police", "Radiohead", map[string]any{"isrc": "ISRC-BBB"})

	perProvider := [][]domain.SearchResult{
		{track1, track2},
	}

	results := FuseAndRank(perProvider, "radiohead", noQualityScorer, nil)

	if len(results) != 2 {
		t.Fatalf("expected 2 separate results, got %d", len(results))
	}
}

func TestFuseAndRank_SingleProvider(t *testing.T) {
	// Single-provider result → low confidence.
	track := trackResult(domain.ProviderDeezer, "dz-1", "Creep", "Radiohead", nil)

	perProvider := [][]domain.SearchResult{
		{track},
	}

	results := FuseAndRank(perProvider, "creep radiohead", noQualityScorer, nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Confidence != domain.ConfidenceLow {
		t.Errorf("expected low confidence for single-provider result, got %s", results[0].Confidence.String())
	}
}

func TestFuseAndRank_RelevanceRanking(t *testing.T) {
	// A result that closely matches the query should rank above one that barely matches.
	// Both are tracks from Deezer (pass hasBrowseableSource).
	// "bohemian" appears in both titles but "rhapsody" only in one.
	exactMatch := trackResult(domain.ProviderDeezer, "dz-1", "Bohemian Rhapsody", "Queen", nil)
	partialMatch := trackResult(domain.ProviderDeezer, "dz-2", "Bohemian Grove", "Queen", nil)

	perProvider := [][]domain.SearchResult{
		// native order: partial first, exact second
		{partialMatch, exactMatch},
	}

	results := FuseAndRank(perProvider, "bohemian rhapsody", noQualityScorer, nil)

	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	// The exact match for "bohemian rhapsody" should rank first.
	if results[0].Title != "Bohemian Rhapsody" {
		t.Errorf("expected 'Bohemian Rhapsody' ranked first, got %q", results[0].Title)
	}
}

func TestFuseAndRank_MultiSourceBoost(t *testing.T) {
	// A multi-source result should rank above a single-source result at similar relevance.
	// We give both the same title/artist and query match, but one has two sources via ISRC merge.
	multiA := trackResult(domain.ProviderDeezer, "dz-1", "Bohemian Rhapsody", "Queen", map[string]any{"isrc": "GBUM71029604"})
	multiB := trackResult(domain.ProviderMusicBrainz, "mb-1", "Bohemian Rhapsody", "Queen", map[string]any{"isrc": "GBUM71029604"})
	single := trackResult(domain.ProviderDeezer, "dz-2", "Bohemian Rhapsody Live", "Queen", nil)

	perProvider := [][]domain.SearchResult{
		{multiA, single},
		{multiB},
	}

	results := FuseAndRank(perProvider, "bohemian rhapsody queen", noQualityScorer, nil)

	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	// The merged (multi-source) result should rank first.
	if len(results[0].Sources) < 2 {
		t.Errorf("expected first result to be multi-source (>=2), got %d sources", len(results[0].Sources))
	}
}

func TestFuseAndRank_WordShareGating(t *testing.T) {
	// A result that shares no words with the query should be filtered out.
	relevant := trackResult(domain.ProviderDeezer, "dz-1", "Yesterday", "Beatles", nil)
	irrelevant := trackResult(domain.ProviderDeezer, "dz-2", "Completely Different Song", "Unknown Artist", nil)

	perProvider := [][]domain.SearchResult{
		{relevant, irrelevant},
	}

	results := FuseAndRank(perProvider, "yesterday beatles", noQualityScorer, nil)

	for _, r := range results {
		if r.Title == "Completely Different Song" {
			t.Errorf("expected 'Completely Different Song' to be filtered out by word-share gating, but it was present")
		}
	}
	// The relevant result should still be present.
	found := false
	for _, r := range results {
		if r.Title == "Yesterday" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'Yesterday' to be present in results")
	}
}

func TestFuseAndRank_EmptyInput(t *testing.T) {
	results := FuseAndRank(nil, "", noQualityScorer, nil)
	if len(results) != 0 {
		t.Errorf("expected empty results for empty input, got %d", len(results))
	}

	results2 := FuseAndRank([][]domain.SearchResult{}, "some query", noQualityScorer, nil)
	if len(results2) != 0 {
		t.Errorf("expected empty results for empty perProvider, got %d", len(results2))
	}
}

func TestRerank(t *testing.T) {
	// Rerank re-sorts by band-based relevance. A result with higher relevance
	// to the query should be ranked first after reranking.
	// We construct two results with different relevance to "bohemian rhapsody".
	lowRelevance := domain.SearchResult{
		Kind:     domain.ResultKindTrack,
		Title:    "Something Else",
		Subtitle: "Bohemian Artist",
		Sources:  []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "1"}},
		Extras:   map[string]any{"_rrf": 0.01},
	}
	highRelevance := domain.SearchResult{
		Kind:     domain.ResultKindTrack,
		Title:    "Bohemian Rhapsody",
		Subtitle: "Queen",
		Sources:  []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "2"}},
		Extras:   map[string]any{"_rrf": 0.02},
	}

	// Input: low relevance first.
	input := []domain.SearchResult{lowRelevance, highRelevance}
	results := Rerank(input, "bohemian rhapsody queen")

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Title != "Bohemian Rhapsody" {
		t.Errorf("expected 'Bohemian Rhapsody' ranked first after rerank, got %q", results[0].Title)
	}
}

func TestFuseAndRank_NonTrackWithoutDeezerFiltered(t *testing.T) {
	// Non-track results without a Deezer source should be filtered by hasBrowseableSource.
	artistOnlyMB := artistResult(domain.ProviderMusicBrainz, "mb-1", "Radiohead", nil)

	perProvider := [][]domain.SearchResult{
		{artistOnlyMB},
	}

	results := FuseAndRank(perProvider, "radiohead", noQualityScorer, nil)

	if len(results) != 0 {
		t.Errorf("expected non-track result without Deezer source to be filtered, got %d results", len(results))
	}
}

func TestFuseAndRank_DifferentMBIDDoNotMerge(t *testing.T) {
	// Two tracks with different MBIDs should NOT merge, even if titles are similar.
	track1 := trackResult(domain.ProviderDeezer, "dz-1", "Creep", "Radiohead", map[string]any{"mbid": "aaa-111"})
	track2 := trackResult(domain.ProviderMusicBrainz, "mb-1", "Creep", "Radiohead", map[string]any{"mbid": "bbb-222"})

	perProvider := [][]domain.SearchResult{
		{track1},
		{track2},
	}

	results := FuseAndRank(perProvider, "creep radiohead", noQualityScorer, nil)

	if len(results) < 2 {
		t.Errorf("expected 2 separate results for different MBIDs, got %d", len(results))
	}
}

func TestFuseAndRank_MergedResultPicksMoreCompleteCanonical(t *testing.T) {
	// When merging, the result with higher completeness should be the canonical (title/subtitle).
	sparse := trackResult(domain.ProviderDeezer, "dz-1", "Creep", "Radiohead", map[string]any{"isrc": "GBAYE0000351"})
	rich := trackResult(domain.ProviderMusicBrainz, "mb-1", "Creep", "Radiohead", map[string]any{
		"isrc":             "GBAYE0000351",
		"album":            "Pablo Honey",
		"duration_seconds": 238,
	})
	rich.ImageURL = "https://img.example.com/creep.jpg"

	perProvider := [][]domain.SearchResult{
		{sparse},
		{rich},
	}

	results := FuseAndRank(perProvider, "creep radiohead", noQualityScorer, nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 merged result, got %d", len(results))
	}
	// The merged result should have the image from the richer entry.
	if results[0].ImageURL != "https://img.example.com/creep.jpg" {
		t.Errorf("expected image from richer result, got %q", results[0].ImageURL)
	}
	// Album extra should be present from the richer entry.
	album := getStringExtra(results[0], "album")
	if album != "Pablo Honey" {
		t.Errorf("expected album 'Pablo Honey' from richer result, got %q", album)
	}
}

func TestCollapseVersions(t *testing.T) {
	t.Run("collapses remix and slowed versions", func(t *testing.T) {
		results := []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "1", "Megaman", "Tay-K", map[string]any{"popularity": int64(80)}),
			trackResult(domain.ProviderDeezer, "2", "Megaman (Remix)", "Tay-K", map[string]any{"popularity": int64(30)}),
			trackResult(domain.ProviderDeezer, "3", "Megaman (Slowed + Reverb)", "Tay-K", map[string]any{"popularity": int64(10)}),
		}
		got := CollapseVersions(results)
		if len(got) != 1 {
			t.Fatalf("expected 1 result, got %d", len(got))
		}
		if got[0].Title != "Megaman" {
			t.Errorf("expected representative 'Megaman', got %q", got[0].Title)
		}
		vc, ok := got[0].Extras["variant_count"].(int)
		if !ok || vc != 3 {
			t.Errorf("expected variant_count=3, got %v", got[0].Extras["variant_count"])
		}
	})

	t.Run("different artists not collapsed", func(t *testing.T) {
		results := []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "1", "Megaman", "Tay-K", map[string]any{"popularity": int64(80)}),
			trackResult(domain.ProviderDeezer, "2", "Megaman", "Other Artist", map[string]any{"popularity": int64(50)}),
		}
		got := CollapseVersions(results)
		if len(got) != 2 {
			t.Fatalf("expected 2 results, got %d", len(got))
		}
	})

	t.Run("feat parenthetical not stripped", func(t *testing.T) {
		results := []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "1", "Song (feat. Artist)", "Main", map[string]any{"popularity": int64(50)}),
			trackResult(domain.ProviderDeezer, "2", "Song", "Main", map[string]any{"popularity": int64(40)}),
		}
		got := CollapseVersions(results)
		if len(got) != 2 {
			t.Fatalf("expected 2 results (feat not stripped), got %d", len(got))
		}
	})

	t.Run("single version no variant count", func(t *testing.T) {
		results := []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "1", "Unique Song", "Artist", map[string]any{}),
		}
		got := CollapseVersions(results)
		if len(got) != 1 {
			t.Fatalf("expected 1 result, got %d", len(got))
		}
		if _, ok := got[0].Extras["variant_count"]; ok {
			t.Error("expected no variant_count for single version")
		}
	})

	t.Run("different kinds not collapsed", func(t *testing.T) {
		results := []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "1", "Megaman", "Tay-K", map[string]any{"popularity": int64(80)}),
			albumResult(domain.ProviderDeezer, "2", "Megaman", "Tay-K", map[string]any{"popularity": int64(50)}),
		}
		got := CollapseVersions(results)
		if len(got) != 2 {
			t.Fatalf("expected 2 results (different kinds), got %d", len(got))
		}
	})

	t.Run("most popular version becomes representative", func(t *testing.T) {
		results := []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "1", "Song (Live)", "Artist", map[string]any{"popularity": int64(10)}),
			trackResult(domain.ProviderDeezer, "2", "Song (Remix)", "Artist", map[string]any{"popularity": int64(90)}),
			trackResult(domain.ProviderDeezer, "3", "Song", "Artist", map[string]any{"popularity": int64(50)}),
		}
		got := CollapseVersions(results)
		if len(got) != 1 {
			t.Fatalf("expected 1 result, got %d", len(got))
		}
		if got[0].Title != "Song (Remix)" {
			t.Errorf("expected most popular version 'Song (Remix)' as representative, got %q", got[0].Title)
		}
	})
}

func TestFuseAndRank_PopularityNormalization(t *testing.T) {
	popular := trackResult(domain.ProviderDeezer, "dz-1", "Blinding Lights", "The Weeknd",
		map[string]any{"nb_fan": int64(5_000_000)})
	unpopular := trackResult(domain.ProviderDeezer, "dz-2", "Blinding Lights Acoustic", "The Weeknd", nil)

	perProvider := [][]domain.SearchResult{
		{unpopular, popular},
	}

	results := FuseAndRank(perProvider, "blinding lights weeknd", noQualityScorer, nil)

	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	pop0 := popularity(results[0])
	pop1 := popularity(results[1])
	if pop0 <= pop1 {
		t.Errorf("expected first result to have higher popularity (%v) than second (%v)", pop0, pop1)
	}
}

func TestApplyRecencyBoost(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	old := time.Now().AddDate(0, 0, -60).Format("2006-01-02")

	t.Run("recent release gets boosted", func(t *testing.T) {
		results := []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "1", "New Song", "Artist", map[string]any{
				"popularity": int64(50), "release_date": today,
			}),
		}
		got := applyRecencyBoost(results)
		pop := popularity(got[0])
		if pop < 54.9 || pop > 55.1 {
			t.Errorf("expected boosted popularity ~55, got %v", pop)
		}
	})

	t.Run("old release not boosted", func(t *testing.T) {
		results := []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "1", "Old Song", "Artist", map[string]any{
				"popularity": int64(50), "release_date": old,
			}),
		}
		got := applyRecencyBoost(results)
		pop := popularity(got[0])
		if pop != 50 {
			t.Errorf("expected unchanged popularity 50, got %v", pop)
		}
	})

	t.Run("no release date not boosted", func(t *testing.T) {
		results := []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "1", "Song", "Artist", map[string]any{
				"popularity": int64(50),
			}),
		}
		got := applyRecencyBoost(results)
		pop := popularity(got[0])
		if pop != 50 {
			t.Errorf("expected unchanged popularity 50, got %v", pop)
		}
	})

	t.Run("capped at 100", func(t *testing.T) {
		results := []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "1", "Hit", "Artist", map[string]any{
				"popularity": int64(98), "release_date": today,
			}),
		}
		got := applyRecencyBoost(results)
		pop := popularity(got[0])
		if pop > 100 {
			t.Errorf("expected popularity capped at 100, got %v", pop)
		}
	})

	t.Run("year only date parsed", func(t *testing.T) {
		results := []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "1", "Song", "Artist", map[string]any{
				"popularity": int64(50), "release_date": "2020",
			}),
		}
		got := applyRecencyBoost(results)
		pop := popularity(got[0])
		if pop != 50 {
			t.Errorf("expected no boost for 2020 release, got %v", pop)
		}
	})

	t.Run("malformed date not boosted", func(t *testing.T) {
		results := []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "1", "Song", "Artist", map[string]any{
				"popularity": int64(50), "release_date": "not-a-date",
			}),
		}
		got := applyRecencyBoost(results)
		pop := popularity(got[0])
		if pop != 50 {
			t.Errorf("expected unchanged popularity for malformed date, got %v", pop)
		}
	})
}

func TestCollapseArtistDuplicates(t *testing.T) {
	t.Run("groups same-name artists keeping highest popularity", func(t *testing.T) {
		results := []domain.SearchResult{
			artistResult(domain.ProviderDeezer, "1", "Che", map[string]any{
				"popularity": float64(80), "disambiguation": "Atlanta rapper",
			}),
			trackResult(domain.ProviderDeezer, "t1", "BA$$", "Che", nil),
			artistResult(domain.ProviderDeezer, "2", "Che", map[string]any{
				"popularity": float64(30), "disambiguation": "Korean singer-songwriter",
			}),
			artistResult(domain.ProviderDeezer, "3", "Che", map[string]any{
				"popularity": float64(10),
			}),
		}
		got := CollapseArtistDuplicates(results)

		if len(got) != 2 {
			t.Fatalf("expected 2 results (1 artist + 1 track), got %d", len(got))
		}
		if got[0].Title != "Che" || got[0].Kind != domain.ResultKindArtist {
			t.Errorf("expected primary artist 'Che', got %q kind=%s", got[0].Title, got[0].Kind)
		}
		collapsed, ok := got[0].Extras["collapsed_artists"]
		if !ok {
			t.Fatal("expected collapsed_artists extra on primary artist")
		}
		list, ok := collapsed.([]map[string]any)
		if !ok {
			t.Fatalf("collapsed_artists wrong type: %T", collapsed)
		}
		if len(list) != 2 {
			t.Errorf("expected 2 collapsed artists, got %d", len(list))
		}
		if got[1].Kind != domain.ResultKindTrack {
			t.Errorf("expected track result preserved, got kind=%s", got[1].Kind)
		}
	})

	t.Run("no grouping when names differ", func(t *testing.T) {
		results := []domain.SearchResult{
			artistResult(domain.ProviderDeezer, "1", "Drake", map[string]any{"popularity": float64(90)}),
			artistResult(domain.ProviderDeezer, "2", "Aurora", map[string]any{"popularity": float64(70)}),
		}
		got := CollapseArtistDuplicates(results)
		if len(got) != 2 {
			t.Errorf("expected 2 distinct artists preserved, got %d", len(got))
		}
		if _, ok := got[0].Extras["collapsed_artists"]; ok {
			t.Error("unexpected collapsed_artists on unique-name artist")
		}
	})

	t.Run("single artist not collapsed", func(t *testing.T) {
		results := []domain.SearchResult{
			artistResult(domain.ProviderDeezer, "1", "Che", map[string]any{"popularity": float64(80)}),
		}
		got := CollapseArtistDuplicates(results)
		if len(got) != 1 {
			t.Errorf("expected 1 result, got %d", len(got))
		}
		if _, ok := got[0].Extras["collapsed_artists"]; ok {
			t.Error("single artist should not have collapsed_artists")
		}
	})
}
