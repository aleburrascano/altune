package service

import (
	"context"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// TestSearchQualityDemo is NOT a pass/fail test — it prints search pipeline
// output so you can visually verify ranking, correction, intent detection,
// and noise stripping behavior. Run with:
//
//	go test ./internal/discovery/service/... -run TestSearchQualityDemo -v
func TestSearchQualityDemo(t *testing.T) {
	vocab := &demoVocab{
		prefixEntries: map[string][]domain.VocabularyEntry{
			"tay k":        {{Term: "Tay-K", TermNorm: "tay k", Kind: "artist", Popularity: 80}},
			"weeknd":       {{Term: "The Weeknd", TermNorm: "weeknd", Kind: "artist", Popularity: 90}},
			"drake":        {{Term: "Drake", TermNorm: "drake", Kind: "artist", Popularity: 95}},
			"taylor swift": {{Term: "Taylor Swift", TermNorm: "taylor swift", Kind: "artist", Popularity: 93}},
		},
		fuzzyEntries: map[string][]domain.VocabularyEntry{
			"megamsn":  {{Term: "Megaman", TermNorm: "megaman", Kind: "track", Popularity: 70}},
			"megaman":  {{Term: "Megaman", TermNorm: "megaman", Kind: "track", Popularity: 70}},
			"weekend":  {{Term: "The Weeknd", TermNorm: "weeknd", Kind: "artist", Popularity: 90}},
			"tay k":    {{Term: "Tay-K", TermNorm: "tay k", Kind: "artist", Popularity: 80}},
			"drak":     {{Term: "Drake", TermNorm: "drake", Kind: "artist", Popularity: 95}},
		},
	}

	// Simulate provider results for "Megaman"
	megamanProviderResults := [][]domain.SearchResult{
		// Deezer results
		{
			trackResult(domain.ProviderDeezer, "dz-1", "Megaman", "Tay-K",
				map[string]any{"nb_fan": int64(500000), "isrc": "US1234567890", "duration": 180}),
			trackResult(domain.ProviderDeezer, "dz-2", "Megaman (Remix)", "Tay-K",
				map[string]any{"nb_fan": int64(100000), "duration": 200}),
			artistResult(domain.ProviderDeezer, "dz-art-1", "Megaman",
				map[string]any{"nb_fan": int64(500)}),
		},
		// Last.fm results
		{
			trackResult(domain.ProviderLastFM, "lfm-1", "Megaman", "Tay-K",
				map[string]any{"listeners": "1500000", "isrc": "US1234567890"}),
			artistResult(domain.ProviderLastFM, "lfm-art-1", "Megaman",
				map[string]any{"listeners": "200"}),
		},
		// MusicBrainz results
		{
			trackResult(domain.ProviderMusicBrainz, "mb-1", "Megaman", "Tay-K",
				map[string]any{"mbid": "abc-123", "isrc": "US1234567890"}),
		},
	}

	scorer := func(r domain.SearchResult) domain.QualityScore {
		return ComputeQualityScore(r, 1.0)
	}

	queries := []struct {
		name      string
		query     string
		providers [][]domain.SearchResult
	}{
		{
			name:      "Basic: 'Megaman' — Tay-K track should beat obscure artist",
			query:     "Megaman",
			providers: megamanProviderResults,
		},
		{
			name:      "Intent: 'Tay-K Megaman' — intent boost should push matching track to #1",
			query:     "tay k megaman",
			providers: megamanProviderResults,
		},
		{
			name:  "Noise: 'megaman official video' — noise stripped, same results as 'megaman'",
			query: "megaman official video",
			providers: megamanProviderResults,
		},
	}

	for _, tc := range queries {
		t.Run(tc.name, func(t *testing.T) {
			cleaned := CleanQuery(tc.query)
			queryNorm := NormalizeForMatch(cleaned)

			t.Logf("\n=== Query: %q ===\n", tc.query)
			t.Logf("  Cleaned:    %q\n", cleaned)
			t.Logf("  Normalized: %q\n", queryNorm)

			// Pre-query correction
			corrSvc := NewCorrectionService(vocab)
			correction := corrSvc.Correct(context.Background(), tc.query)
			if correction != nil {
				corrNorm := NormalizeForMatch(correction.Corrected)
				if corrNorm != queryNorm {
					t.Logf("  Correction: %q → %q (confidence: %.2f)\n",
						tc.query, correction.Corrected, correction.Confidence)
				}
			}

			// Intent detection
			intent := DetectIntent(context.Background(), queryNorm, vocab)
			if intent != nil {
				t.Logf("  Intent:     artist=%q track=%q (confidence: %.2f)\n",
					intent.Artist, intent.Track, intent.Confidence)
			} else {
				t.Logf("  Intent:     none detected\n")
			}

			// FuseAndRank
			results := FuseAndRank(tc.providers, queryNorm, scorer, intent)
			t.Logf("  Results (%d):\n", len(results))
			for i, r := range results {
				pop := popularity(r)
				rel := relevanceScore(r, queryNorm)
				band := roundBand(rel)
				variantCount := 0
				if vc, ok := r.Extras["variant_count"]; ok {
					if v, ok := vc.(int); ok {
						variantCount = v
					}
				}
				t.Logf("    #%d [%s] %q by %q  pop=%.0f rel=%.2f band=%.2f sources=%d",
					i+1, r.Kind.String(), r.Title, r.Subtitle, pop, rel, band, len(r.Sources))
				if variantCount > 0 {
					t.Logf(" variants=%d", variantCount)
				}
				t.Log("")
			}
		})
	}

	// Correction demo
	t.Run("Correction: 'Megamsn' → should correct to 'Megaman'", func(t *testing.T) {
		corrSvc := NewCorrectionService(vocab)
		result := corrSvc.Correct(context.Background(), "Megamsn")
		t.Logf("\n=== Correction: 'Megamsn' ===\n")
		if result != nil {
			t.Logf("  Corrected to: %q (confidence: %.2f)\n", result.Corrected, result.Confidence)
		} else {
			t.Logf("  No correction found\n")
		}
	})

	t.Run("Correction: 'The Weekend' → should correct to 'The Weeknd' (phonetic)", func(t *testing.T) {
		corrSvc := NewCorrectionService(vocab)
		result := corrSvc.Correct(context.Background(), "The Weekend")
		t.Logf("\n=== Correction: 'The Weekend' ===\n")
		if result != nil {
			t.Logf("  Corrected to: %q (confidence: %.2f)\n", result.Corrected, result.Confidence)
		} else {
			t.Logf("  No correction found (vocabulary may not have phonetic index)\n")
		}
	})
}

// demoVocab is a mock that supports both prefix and fuzzy lookups for demo purposes.
type demoVocab struct {
	prefixEntries map[string][]domain.VocabularyEntry
	fuzzyEntries  map[string][]domain.VocabularyEntry
}

func (v *demoVocab) Add(_ context.Context, _ domain.VocabularyEntry) error { return nil }
func (v *demoVocab) BulkAdd(_ context.Context, _ []domain.VocabularyEntry) error { return nil }

func (v *demoVocab) SuggestByPrefix(_ context.Context, prefix string, limit int) ([]domain.VocabularyEntry, error) {
	entries := v.prefixEntries[prefix]
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func (v *demoVocab) FindClosest(_ context.Context, query string, limit int) ([]domain.VocabularyEntry, error) {
	entries := v.fuzzyEntries[query]
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}
