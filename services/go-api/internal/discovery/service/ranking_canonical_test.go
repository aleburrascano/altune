package service

import (
	"fmt"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// TestCanonicalRankingRegression runs the canonical query list from CLAUDE.md.
// Each test case asserts the #1 result's kind and a substring in its title or
// subtitle. These are the queries the pipeline MUST get right — any change
// that breaks one of these is a ranking regression.
//
// Run with: go test ./internal/discovery/service/... -run TestCanonicalRanking -v
func TestCanonicalRankingRegression(t *testing.T) {
	tests := []struct {
		query          string
		providers      [][]domain.SearchResult
		wantKind       domain.ResultKind
		wantContains   string // substring in title or subtitle of #1
		wantPosition   int    // 0-indexed; default 0 (must be #1)
		description    string
	}{
		{
			query: "Humble",
			providers: humbleProviders(),
			wantKind:     domain.ResultKindTrack,
			wantContains: "Kendrick",
			description:  "#1 must be HUMBLE. by Kendrick Lamar, not the niche artist",
		},
		{
			query: "Scorpion",
			providers: scorpionProviders(),
			wantKind:     domain.ResultKindAlbum,
			wantContains: "Drake",
			description:  "#1 must be album Scorpion by Drake",
		},
		{
			query: "Bohemian Rhapsody",
			providers: bohemianProviders(),
			wantKind:     domain.ResultKindTrack,
			wantContains: "Queen",
			description:  "#1 must be Bohemian Rhapsody by Queen",
		},
		{
			query: "Circles",
			providers: circlesProviders(),
			wantKind:     domain.ResultKindTrack,
			wantContains: "Post Malone",
			description:  "#1 must be Circles by Post Malone",
		},
		{
			query: "Drake",
			providers: drakeProviders(),
			wantKind:     domain.ResultKindArtist,
			wantContains: "Drake",
			description:  "#1 must be artist Drake",
		},
		{
			query: "Bad Bunny",
			providers: badBunnyProviders(),
			wantKind:     domain.ResultKindArtist,
			wantContains: "Bad Bunny",
			description:  "#1 must be artist Bad Bunny",
		},
		{
			query: "Blinding Lights",
			providers: blindingLightsProviders(),
			wantKind:     domain.ResultKindTrack,
			wantContains: "Weeknd",
			description:  "#1 must be Blinding Lights by The Weeknd",
		},
		{
			query: "Tay-K Megaman",
			providers: taykMegamanProviders(),
			wantKind:     domain.ResultKindTrack,
			wantContains: "Tay-K",
			description:  "#1 must be Megaman by Tay-K",
		},
		{
			query: "Kendrick Lamar Humble",
			providers: kendrickHumbleProviders(),
			wantKind:     domain.ResultKindTrack,
			wantContains: "Kendrick",
			description:  "#1 must be HUMBLE. by Kendrick Lamar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			queryNorm := NormalizeForMatch(tt.query)
			results := FuseAndRank(tt.providers, queryNorm, noQualityScorer, nil)

			if len(results) == 0 {
				t.Fatalf("FAIL: %s — got 0 results", tt.description)
			}

			pos := tt.wantPosition
			r := results[pos]
			found := r.Kind == tt.wantKind &&
				(strings.Contains(r.Title, tt.wantContains) ||
					strings.Contains(r.Subtitle, tt.wantContains))

			if !found {
				t.Errorf("FAIL: %s\n  got #%d: [%s] %q by %q\n  want: [%s] containing %q",
					tt.description,
					pos+1, r.Kind.String(), r.Title, r.Subtitle,
					tt.wantKind.String(), tt.wantContains)
				for i := 0; i < min(5, len(results)); i++ {
					t.Logf("  #%d [%s] %q by %q pop=%.0f",
						i+1, results[i].Kind, results[i].Title, results[i].Subtitle, popularity(results[i]))
				}
			}
		})
	}
}

// TestRankingRegressionReport runs all canonical queries and prints a summary
// report. This is informational — it always passes, but shows the ranking
// health at a glance. Run with:
//
//	go test ./internal/discovery/service/... -run TestRankingRegressionReport -v
func TestRankingRegressionReport(t *testing.T) {
	type expectation struct {
		query        string
		wantKind     domain.ResultKind
		wantContains string
	}

	queries := []expectation{
		{"Humble", domain.ResultKindTrack, "Kendrick"},
		{"Scorpion", domain.ResultKindAlbum, "Drake"},
		{"Bohemian Rhapsody", domain.ResultKindTrack, "Queen"},
		{"Circles", domain.ResultKindTrack, "Post Malone"},
		{"Drake", domain.ResultKindArtist, "Drake"},
		{"Bad Bunny", domain.ResultKindArtist, "Bad Bunny"},
		{"Blinding Lights", domain.ResultKindTrack, "Weeknd"},
		{"Tay-K Megaman", domain.ResultKindTrack, "Tay-K"},
		{"Kendrick Lamar Humble", domain.ResultKindTrack, "Kendrick"},
	}

	providerSets := map[string][][]domain.SearchResult{
		"Humble":                humbleProviders(),
		"Scorpion":              scorpionProviders(),
		"Bohemian Rhapsody":     bohemianProviders(),
		"Circles":               circlesProviders(),
		"Drake":                 drakeProviders(),
		"Bad Bunny":             badBunnyProviders(),
		"Blinding Lights":       blindingLightsProviders(),
		"Tay-K Megaman":         taykMegamanProviders(),
		"Kendrick Lamar Humble": kendrickHumbleProviders(),
	}

	passed, failed := 0, 0
	t.Log("\n=== Ranking Regression Report ===")
	t.Log(fmt.Sprintf("%-30s %-8s %-6s %s", "QUERY", "STATUS", "KIND", "TOP RESULT"))
	t.Log(strings.Repeat("-", 80))

	for _, q := range queries {
		providers := providerSets[q.query]
		queryNorm := NormalizeForMatch(q.query)
		results := FuseAndRank(providers, queryNorm, noQualityScorer, nil)

		if len(results) == 0 {
			t.Log(fmt.Sprintf("%-30s %-8s %-6s %s", q.query, "FAIL", "-", "no results"))
			failed++
			continue
		}

		r := results[0]
		ok := r.Kind == q.wantKind &&
			(strings.Contains(r.Title, q.wantContains) || strings.Contains(r.Subtitle, q.wantContains))

		status := "PASS"
		if !ok {
			status = "FAIL"
			failed++
		} else {
			passed++
		}

		t.Log(fmt.Sprintf("%-30s %-8s %-6s [%s] %q by %q (pop=%.0f)",
			q.query, status, r.Kind.String(), r.Kind.String(), r.Title, r.Subtitle, popularity(r)))
	}

	t.Log(strings.Repeat("-", 80))
	t.Log(fmt.Sprintf("Total: %d/%d passed", passed, passed+failed))
}

// --- Provider fixture sets ---

func humbleProviders() [][]domain.SearchResult {
	return [][]domain.SearchResult{
		{
			artistResult(domain.ProviderDeezer, "dz-art-humble", "Humble",
				map[string]any{"nb_fan": int64(323)}),
			trackResult(domain.ProviderDeezer, "dz-trk-humble", "HUMBLE.", "Kendrick Lamar",
				map[string]any{"rank": int64(781_820)}),
		},
		{artistResult(domain.ProviderMusicBrainz, "mb-art-humble", "Humble", nil)},
		{artistResult(domain.ProviderSoundCloud, "sc-art-humble", "Humble", nil)},
		{artistResult(domain.ProviderITunes, "it-art-humble", "Humble", nil)},
	}
}

func scorpionProviders() [][]domain.SearchResult {
	return [][]domain.SearchResult{
		{
			albumResult(domain.ProviderDeezer, "dz-album-scorpion", "Scorpion", "Drake", nil),
			trackResult(domain.ProviderDeezer, "dz-trk-scorp1", "Scorpion", "Eve",
				map[string]any{"rank": int64(50_000)}),
			trackResult(domain.ProviderDeezer, "dz-trk-scorp2", "Scorpion", "Scorpion Child",
				map[string]any{"rank": int64(40_000)}),
		},
	}
}

func bohemianProviders() [][]domain.SearchResult {
	return [][]domain.SearchResult{
		{
			trackResult(domain.ProviderDeezer, "dz-bohemian", "Bohemian Rhapsody", "Queen",
				map[string]any{"rank": int64(900_000), "isrc": "GBUM71029604"}),
			trackResult(domain.ProviderDeezer, "dz-bohemian-cover", "Bohemian Rhapsody", "Panic! At The Disco",
				map[string]any{"rank": int64(200_000)}),
		},
		{
			trackResult(domain.ProviderMusicBrainz, "mb-bohemian", "Bohemian Rhapsody", "Queen",
				map[string]any{"isrc": "GBUM71029604", "mbid": "b1a9c0e6-1234-5678-9abc-def012345678"}),
		},
	}
}

func circlesProviders() [][]domain.SearchResult {
	return [][]domain.SearchResult{
		{
			trackResult(domain.ProviderDeezer, "dz-circles", "Circles", "Post Malone",
				map[string]any{"rank": int64(850_000), "isrc": "USRC11900084"}),
			trackResult(domain.ProviderDeezer, "dz-circles-mac", "Circles", "Mac Miller",
				map[string]any{"rank": int64(400_000)}),
			albumResult(domain.ProviderDeezer, "dz-alb-circles", "Circles", "Mac Miller", nil),
			artistResult(domain.ProviderDeezer, "dz-art-circles", "Circles",
				map[string]any{"nb_fan": int64(200)}),
		},
		{
			trackResult(domain.ProviderITunes, "it-circles", "Circles", "Post Malone",
				map[string]any{"isrc": "USRC11900084"}),
		},
	}
}

func drakeProviders() [][]domain.SearchResult {
	return [][]domain.SearchResult{
		{
			artistResult(domain.ProviderDeezer, "dz-art-drake", "Drake",
				map[string]any{"nb_fan": int64(25_000_000)}),
			trackResult(domain.ProviderDeezer, "dz-trk-drake1", "Drake", "Boring Band",
				map[string]any{"rank": int64(0)}),
		},
		{artistResult(domain.ProviderMusicBrainz, "mb-art-drake", "Drake", nil)},
		{artistResult(domain.ProviderSoundCloud, "sc-art-drake", "Drake", nil)},
		{artistResult(domain.ProviderLastFM, "lfm-art-drake", "Drake", nil)},
		{artistResult(domain.ProviderITunes, "it-art-drake", "Drake", nil)},
		{artistResult(domain.ProviderTheAudioDB, "adb-art-drake", "Drake", nil)},
	}
}

func badBunnyProviders() [][]domain.SearchResult {
	return [][]domain.SearchResult{
		{
			artistResult(domain.ProviderDeezer, "dz-art-badbunny", "Bad Bunny",
				map[string]any{"nb_fan": int64(40_000_000)}),
			trackResult(domain.ProviderDeezer, "dz-trk-bb1", "Bad Bunny", "Some Cover Artist",
				map[string]any{"rank": int64(10_000)}),
		},
		{artistResult(domain.ProviderMusicBrainz, "mb-art-badbunny", "Bad Bunny", nil)},
		{artistResult(domain.ProviderSoundCloud, "sc-art-badbunny", "Bad Bunny", nil)},
		{artistResult(domain.ProviderLastFM, "lfm-art-badbunny", "Bad Bunny", nil)},
		{artistResult(domain.ProviderITunes, "it-art-badbunny", "Bad Bunny", nil)},
	}
}

func blindingLightsProviders() [][]domain.SearchResult {
	return [][]domain.SearchResult{
		{
			trackResult(domain.ProviderDeezer, "dz-bl", "Blinding Lights", "The Weeknd",
				map[string]any{"rank": int64(950_000), "isrc": "USUM71922973"}),
			trackResult(domain.ProviderDeezer, "dz-bl-cover", "Blinding Lights", "Piano Cover Band",
				map[string]any{"rank": int64(100_000)}),
		},
		{
			trackResult(domain.ProviderMusicBrainz, "mb-bl", "Blinding Lights", "The Weeknd",
				map[string]any{"isrc": "USUM71922973"}),
		},
		{
			trackResult(domain.ProviderITunes, "it-bl", "Blinding Lights", "The Weeknd",
				map[string]any{"isrc": "USUM71922973"}),
		},
	}
}

func taykMegamanProviders() [][]domain.SearchResult {
	return [][]domain.SearchResult{
		{
			trackResult(domain.ProviderDeezer, "dz-megaman", "Megaman", "Tay-K",
				map[string]any{"rank": int64(500_000), "isrc": "US1234567890"}),
			artistResult(domain.ProviderDeezer, "dz-art-megaman", "Megaman",
				map[string]any{"nb_fan": int64(500)}),
		},
		{
			trackResult(domain.ProviderLastFM, "lfm-megaman", "Megaman", "Tay-K",
				map[string]any{"isrc": "US1234567890"}),
		},
	}
}

func kendrickHumbleProviders() [][]domain.SearchResult {
	return [][]domain.SearchResult{
		{
			trackResult(domain.ProviderDeezer, "dz-humble", "HUMBLE.", "Kendrick Lamar",
				map[string]any{"rank": int64(781_820), "isrc": "USUM71700626"}),
			trackResult(domain.ProviderDeezer, "dz-humble2", "Humble and Kind", "Tim McGraw",
				map[string]any{"rank": int64(200_000)}),
		},
		{
			trackResult(domain.ProviderMusicBrainz, "mb-humble", "HUMBLE.", "Kendrick Lamar",
				map[string]any{"isrc": "USUM71700626"}),
		},
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
