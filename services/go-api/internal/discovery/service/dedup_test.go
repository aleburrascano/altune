package service

import (
	"testing"

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

	results := FuseAndRank(perProvider, "bohemian rhapsody queen", noQualityScorer)

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

	results := FuseAndRank(perProvider, "paranoid android radiohead", noQualityScorer)

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

func TestFuseAndRank_ArtistNameMerge(t *testing.T) {
	// Two providers return the same artist by name (artists lack ISRC).
	// Non-track results need Deezer source to pass hasBrowseableSource.
	// Expected: merged with medium confidence.
	deezerArtist := artistResult(domain.ProviderDeezer, "dz-artist-1", "The Weeknd", nil)
	mbArtist := artistResult(domain.ProviderMusicBrainz, "mb-artist-1", "The Weeknd", nil)

	perProvider := [][]domain.SearchResult{
		{deezerArtist},
		{mbArtist},
	}

	results := FuseAndRank(perProvider, "weeknd", noQualityScorer)

	if len(results) != 1 {
		t.Fatalf("expected 1 merged artist result, got %d", len(results))
	}
	r := results[0]
	if r.Confidence != domain.ConfidenceMedium {
		t.Errorf("expected confidence medium for artist name merge, got %s", r.Confidence.String())
	}
	if len(r.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(r.Sources))
	}
}

func TestFuseAndRank_NoMerge(t *testing.T) {
	// Two different tracks from same provider stay separate.
	track1 := trackResult(domain.ProviderDeezer, "dz-1", "Creep", "Radiohead", map[string]any{"isrc": "ISRC-AAA"})
	track2 := trackResult(domain.ProviderDeezer, "dz-2", "Karma Police", "Radiohead", map[string]any{"isrc": "ISRC-BBB"})

	perProvider := [][]domain.SearchResult{
		{track1, track2},
	}

	results := FuseAndRank(perProvider, "radiohead", noQualityScorer)

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

	results := FuseAndRank(perProvider, "creep radiohead", noQualityScorer)

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

	results := FuseAndRank(perProvider, "bohemian rhapsody", noQualityScorer)

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

	results := FuseAndRank(perProvider, "bohemian rhapsody queen", noQualityScorer)

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

	results := FuseAndRank(perProvider, "yesterday beatles", noQualityScorer)

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
	results := FuseAndRank(nil, "", noQualityScorer)
	if len(results) != 0 {
		t.Errorf("expected empty results for empty input, got %d", len(results))
	}

	results2 := FuseAndRank([][]domain.SearchResult{}, "some query", noQualityScorer)
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

	results := FuseAndRank(perProvider, "radiohead", noQualityScorer)

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

	results := FuseAndRank(perProvider, "creep radiohead", noQualityScorer)

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

	results := FuseAndRank(perProvider, "creep radiohead", noQualityScorer)

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
