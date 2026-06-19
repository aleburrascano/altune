package service

import (
	"context"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

func TestRankingRegression_HumbleAmbiguousSingleWord(t *testing.T) {
	// Regression: 4-source artist "Humble" (nb_fan=323, pop~31) used to outrank
	// single-source track "HUMBLE." by Kendrick (rank=781820, pop~98) because
	// multi-source was ranked above popularity in the sort key.
	perProvider := [][]domain.SearchResult{
		{
			artistResult(domain.ProviderDeezer, "dz-art-humble", "Humble",
				map[string]any{"nb_fan": int64(323)}),
			trackResult(domain.ProviderDeezer, "dz-trk-humble", "HUMBLE.", "Kendrick Lamar",
				map[string]any{"rank": int64(781_820)}),
		},
		{
			artistResult(domain.ProviderMusicBrainz, "mb-art-humble", "Humble", nil),
		},
		{
			artistResult(domain.ProviderSoundCloud, "sc-art-humble", "Humble", nil),
		},
		{
			artistResult(domain.ProviderITunes, "it-art-humble", "Humble", nil),
		},
	}

	results := FuseAndRank(perProvider, "humble", noQualityScorer, nil)

	if len(results) == 0 {
		t.Fatal("expected results, got 0")
	}
	if results[0].Kind != domain.ResultKindTrack {
		t.Fatalf("expected #1 to be track, got %s (popularity must beat multi-source)",
			results[0].Kind.String())
	}
	if !strings.Contains(results[0].Subtitle, "Kendrick") {
		t.Errorf("expected #1 subtitle to contain 'Kendrick', got %q", results[0].Subtitle)
	}
}

func TestRankingRegression_ScorpionAlbumInBlended(t *testing.T) {
	// Regression: albums got zero popularity because Deezer album search returns
	// nb_fan=0. After fix, albums without metrics get positionalPopularity from
	// their kind-local Deezer position (pos 0 → pop 75).
	perProvider := [][]domain.SearchResult{
		{
			albumResult(domain.ProviderDeezer, "dz-album-scorpion", "Scorpion", "Drake", nil),
			trackResult(domain.ProviderDeezer, "dz-trk-1", "Scorpion", "Eve",
				map[string]any{"rank": int64(50_000)}),
			trackResult(domain.ProviderDeezer, "dz-trk-2", "Scorpion", "Scorpion Child",
				map[string]any{"rank": int64(40_000)}),
			trackResult(domain.ProviderDeezer, "dz-trk-3", "Scorpion", "Unknown Artist",
				map[string]any{"rank": int64(10_000)}),
			trackResult(domain.ProviderDeezer, "dz-trk-4", "Scorpion", "Another Band",
				map[string]any{"rank": int64(5_000)}),
		},
	}

	results := FuseAndRank(perProvider, "scorpion", noQualityScorer, nil)

	if len(results) < 3 {
		t.Fatalf("expected at least 3 results, got %d", len(results))
	}
	found := false
	for i := 0; i < 3; i++ {
		if results[i].Kind == domain.ResultKindAlbum && results[i].Title == "Scorpion" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected album 'Scorpion' in top 3; got [%s %q, %s %q, %s %q]",
			results[0].Kind, results[0].Title,
			results[1].Kind, results[1].Title,
			results[2].Kind, results[2].Title)
	}
}

func TestRankingRegression_DrakeArtistStaysFirst(t *testing.T) {
	// Artist "Drake" (nb_fan=25M, pop~92, 6 sources) must outrank niche
	// tracks named "Drake" that only have positional popularity.
	perProvider := [][]domain.SearchResult{
		{
			artistResult(domain.ProviderDeezer, "dz-art-drake", "Drake",
				map[string]any{"nb_fan": int64(25_000_000)}),
			trackResult(domain.ProviderDeezer, "dz-trk-drake1", "Drake", "Boring Band",
				map[string]any{"rank": int64(0)}),
			trackResult(domain.ProviderDeezer, "dz-trk-drake2", "Drake", "Nobody Special",
				map[string]any{"rank": int64(0)}),
		},
		{
			artistResult(domain.ProviderMusicBrainz, "mb-art-drake", "Drake", nil),
		},
		{
			artistResult(domain.ProviderSoundCloud, "sc-art-drake", "Drake", nil),
		},
		{
			artistResult(domain.ProviderLastFM, "lfm-art-drake", "Drake", nil),
		},
		{
			artistResult(domain.ProviderITunes, "it-art-drake", "Drake", nil),
		},
		{
			artistResult(domain.ProviderTheAudioDB, "adb-art-drake", "Drake", nil),
		},
	}

	results := FuseAndRank(perProvider, "drake", noQualityScorer, nil)

	if len(results) == 0 {
		t.Fatal("expected results, got 0")
	}
	if results[0].Kind != domain.ResultKindArtist {
		t.Fatalf("expected #1 to be artist, got %s", results[0].Kind.String())
	}
	if results[0].Title != "Drake" {
		t.Errorf("expected #1 title 'Drake', got %q", results[0].Title)
	}
}

func TestRankingRegression_DeezerRankDirection(t *testing.T) {
	t.Run("higher rank produces higher popularity", func(t *testing.T) {
		popHigh := NormalizePopularity(map[string]any{"rank": int64(800_000)})
		popLow := NormalizePopularity(map[string]any{"rank": int64(50_000)})
		if popHigh <= popLow {
			t.Errorf("rank=800000 (pop=%d) must score higher than rank=50000 (pop=%d)",
				popHigh, popLow)
		}
	})

	t.Run("pipeline ranks higher-rank track first", func(t *testing.T) {
		perProvider := [][]domain.SearchResult{
			{
				trackResult(domain.ProviderDeezer, "dz-niche", "Song", "Niche",
					map[string]any{"isrc": "NICHE001", "rank": int64(50_000)}),
				trackResult(domain.ProviderDeezer, "dz-pop", "Song", "Popular",
					map[string]any{"isrc": "POP001", "rank": int64(800_000)}),
			},
		}

		results := FuseAndRank(perProvider, "song", noQualityScorer, nil)

		if len(results) < 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
		if results[0].Subtitle != "Popular" {
			t.Errorf("expected rank=800000 track first, got subtitle %q", results[0].Subtitle)
		}
	})
}

func TestRankingRegression_EnrichmentPreservesPopularity(t *testing.T) {
	t.Run("resolver returns zero keeps existing", func(t *testing.T) {
		svc := &SearchMusicService{
			popularityResolver: &mockPopularityResolver{
				getPopularityFn: func(_ context.Context, _, _ string) (int64, error) {
					return 0, nil
				},
			},
		}
		result := trackResult(domain.ProviderDeezer, "dz-1", "Hit Song", "Star",
			map[string]any{"popularity": int64(75)})
		result.ImageURL = "https://example.com/cover.jpg"

		enriched := svc.enrichOne(context.Background(), result)
		if got := popularity(enriched); got != 75 {
			t.Errorf("expected popularity preserved at 75, got %v", got)
		}
	})

	t.Run("resolver returns lower keeps existing", func(t *testing.T) {
		// Regression: enrichment used to unconditionally overwrite popularity,
		// so a resolver returning 69 would replace a Deezer-computed 98.
		svc := &SearchMusicService{
			popularityResolver: &mockPopularityResolver{
				getPopularityFn: func(_ context.Context, _, _ string) (int64, error) {
					return 69, nil
				},
			},
		}
		result := trackResult(domain.ProviderDeezer, "dz-1", "bad guy", "Billie Eilish",
			map[string]any{"popularity": int64(98)})
		result.ImageURL = "https://example.com/cover.jpg"

		enriched := svc.enrichOne(context.Background(), result)
		if got := popularity(enriched); got != 98 {
			t.Errorf("expected max(69, 98) = 98, got %v", got)
		}
	})

	t.Run("resolver returns higher replaces existing", func(t *testing.T) {
		svc := &SearchMusicService{
			popularityResolver: &mockPopularityResolver{
				getPopularityFn: func(_ context.Context, _, _ string) (int64, error) {
					return 95, nil
				},
			},
		}
		result := trackResult(domain.ProviderDeezer, "dz-1", "Song", "Artist",
			map[string]any{"popularity": int64(60)})
		result.ImageURL = "https://example.com/cover.jpg"

		enriched := svc.enrichOne(context.Background(), result)
		if got := popularity(enriched); got != 95 {
			t.Errorf("expected max(95, 60) = 95, got %v", got)
		}
	})
}

func TestRankingRegression_PopBandingFavorsSources(t *testing.T) {
	// Regression: cover versions with 1-2 points more popularity beat the
	// canonical original. With 5-point pop banding + source bonus, the
	// multi-source original wins.
	original := trackResult(domain.ProviderDeezer, "dz-orig", "Smells Like Teen Spirit", "Nirvana",
		map[string]any{"isrc": "USGF19942501", "rank": int64(750_000)})
	origMB := trackResult(domain.ProviderMusicBrainz, "mb-orig", "Smells Like Teen Spirit", "Nirvana",
		map[string]any{"isrc": "USGF19942501"})
	cover := trackResult(domain.ProviderDeezer, "dz-cover", "Smells Like Teen Spirit", "Bossa Nova Covers",
		map[string]any{"rank": int64(780_000)})

	perProvider := [][]domain.SearchResult{
		{cover, original},
		{origMB},
	}

	results := FuseAndRank(perProvider, "smells like teen spirit", noQualityScorer, nil)

	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	if results[0].Subtitle != "Nirvana" {
		t.Errorf("expected multi-source Nirvana to beat single-source cover, got #1 by %q",
			results[0].Subtitle)
	}
}

func TestRankingRegression_SourceBonusLiftsMultiSourceArtist(t *testing.T) {
	// A globally famous artist appearing from multiple providers with the same
	// MBID should merge and beat a single-source track with a coincidentally
	// matching name. Artists only merge on MBID — name-only merge is disabled.
	santanaMBID := "5f32a4a4-c04e-4c18-86af-b5c4bc18014c"
	perProvider := [][]domain.SearchResult{
		{
			artistResult(domain.ProviderDeezer, "dz-art-santana", "Santana",
				map[string]any{"nb_fan": int64(20_000_000), "mbid": santanaMBID}),
			trackResult(domain.ProviderDeezer, "dz-trk-santana", "Santana", "Alonzo",
				map[string]any{"rank": int64(900_000)}),
		},
		{artistResult(domain.ProviderMusicBrainz, "mb-art-santana", "Santana",
			map[string]any{"mbid": santanaMBID})},
		{artistResult(domain.ProviderLastFM, "lfm-art-santana", "Santana",
			map[string]any{"mbid": santanaMBID})},
	}

	results := FuseAndRank(perProvider, "santana", noQualityScorer, nil)

	if len(results) == 0 {
		t.Fatal("expected results, got 0")
	}
	if results[0].Kind != domain.ResultKindArtist {
		t.Errorf("expected MBID-merged artist to beat single-source track, got #1 kind=%s title=%q by=%q",
			results[0].Kind, results[0].Title, results[0].Subtitle)
	}
}

func TestRankingRegression_SourceBonusDoesNotOverrideLargePopGap(t *testing.T) {
	// A niche 4-source artist with low popularity must NOT beat a massively
	// popular single-source track. The source bonus must be bounded.
	perProvider := [][]domain.SearchResult{
		{
			artistResult(domain.ProviderDeezer, "dz-art-niche", "Niche",
				map[string]any{"nb_fan": int64(500)}),
			trackResult(domain.ProviderDeezer, "dz-trk-hit", "Niche", "Mega Star",
				map[string]any{"rank": int64(850_000)}),
		},
		{artistResult(domain.ProviderMusicBrainz, "mb-art-niche", "Niche", nil)},
		{artistResult(domain.ProviderSoundCloud, "sc-art-niche", "Niche", nil)},
		{artistResult(domain.ProviderITunes, "it-art-niche", "Niche", nil)},
	}

	results := FuseAndRank(perProvider, "niche", noQualityScorer, nil)

	if len(results) == 0 {
		t.Fatal("expected results, got 0")
	}
	if results[0].Kind != domain.ResultKindTrack {
		t.Errorf("expected popular track to beat niche 4-source artist, got #1 kind=%s",
			results[0].Kind)
	}
}

func TestRankingRegression_AdaptiveBanding(t *testing.T) {
	t.Run("3-point bands above 90 separate mega-hits", func(t *testing.T) {
		if bandPop(99) == bandPop(95) {
			t.Errorf("pop 99 (band %.0f) and pop 95 (band %.0f) must be in different bands",
				bandPop(99), bandPop(95))
		}
	})

	t.Run("5-point bands below 90 suppress noise", func(t *testing.T) {
		if bandPop(72) != bandPop(74) {
			t.Errorf("pop 72 (band %.0f) and pop 74 (band %.0f) should be in the same band",
				bandPop(72), bandPop(74))
		}
	})

	t.Run("boundary: 90 uses narrow banding", func(t *testing.T) {
		if bandPop(90) == bandPop(85) {
			t.Errorf("pop 90 (band %.0f) and pop 85 (band %.0f) must be in different bands",
				bandPop(90), bandPop(85))
		}
	})

	t.Run("pop 99 beats pop 95 in ranking", func(t *testing.T) {
		// Regression: 5-point bands collapsed pop 95 and 99 into the same band,
		// letting multi-source covers beat single-source originals at the top.
		// rank=950000 → pop 99, rank=550000 → pop 95 via logNormalize.
		megaHit := trackResult(domain.ProviderDeezer, "dz-mega", "Shape of You", "Ed Sheeran",
			map[string]any{"rank": int64(950_000)})
		cover := trackResult(domain.ProviderDeezer, "dz-cover", "Shape of You", "Jamie Cullum",
			map[string]any{"isrc": "COVER00001", "rank": int64(550_000)})
		coverMB := trackResult(domain.ProviderMusicBrainz, "mb-cover", "Shape of You", "Jamie Cullum",
			map[string]any{"isrc": "COVER00001"})

		perProvider := [][]domain.SearchResult{
			{megaHit, cover},
			{coverMB},
		}

		results := FuseAndRank(perProvider, "shape of you", noQualityScorer, nil)

		if len(results) < 2 {
			t.Fatalf("expected at least 2 results, got %d", len(results))
		}
		if results[0].Subtitle != "Ed Sheeran" {
			t.Errorf("expected pop-99 Ed Sheeran to beat pop-95 multi-source cover, got #1 by %q",
				results[0].Subtitle)
		}
	})
}

func TestRankingRegression_PreCorrectionDisabled(t *testing.T) {
	// preQueryCorrection is dead code — Execute must pass the original query
	// to providers without rewriting it.
	var receivedQuery string
	provider := &mockSearchProvider{
		name:           domain.ProviderDeezer,
		supportedKinds: map[domain.ResultKind]bool{domain.ResultKindTrack: true},
		searchFn: func(_ context.Context, query string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
			receivedQuery = query
			return []domain.SearchResult{
				trackResult(domain.ProviderDeezer, "d1", "Bohemian Rhapsody", "Queen", map[string]any{}),
			}, nil
		},
	}

	svc := smNewService([]ports.SearchProvider{provider}, nil, nil)
	query := smTestQuery(t, "Bohemian Rhapsody",
		map[domain.ResultKind]bool{domain.ResultKindTrack: true}, 10)

	out, err := svc.Execute(context.Background(), smUserID(), query, false)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if receivedQuery != "Bohemian Rhapsody" {
		t.Errorf("provider received %q, want original %q (pre-correction may have rewritten it)",
			receivedQuery, "Bohemian Rhapsody")
	}
	if out.CorrectedQuery != "" {
		t.Errorf("expected empty CorrectedQuery, got %q", out.CorrectedQuery)
	}
}
