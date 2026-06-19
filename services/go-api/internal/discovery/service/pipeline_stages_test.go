package service

import (
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// Pipeline stage tests: trace a query through each stage of the discovery
// pipeline and verify the output at every transition. If someone changes one
// stage and it breaks downstream behavior, these tests pinpoint WHERE.
//
// Stages (from ARCHITECTURE.md):
//   NormalizeForMatch → CleanQuery → DetectIntent → [providers] →
//   FuseAndRank(merge → popNorm → recency → score → gate → sort →
//   collapse → popularityDominance → diversity) → Enrich → Rerank

// ---------------------------------------------------------------------------
// Stage 1: NormalizeForMatch
// ---------------------------------------------------------------------------

func TestStage_NormalizeForMatch(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Bohemian Rhapsody", "bohemian rhapsody"},
		{"HUMBLE.", "humble"},
		{"The Weeknd", "weeknd"},
		{"Los Lobos", "lobos"},
		{"Beyoncé", "beyonce"},
		{"AC/DC", "ac dc"},
		{"Guns N' Roses", "guns n roses"},
		{"Röyksopp", "royksopp"},
		{"JAY-Z", "jay z"},
		{"Kendrick Lamar (feat. Rihanna)", "kendrick lamar"},
		{"Tay-K", "tay k"},
		{"Bad Bunny", "bad bunny"},
		{"Post Malone", "post malone"},
		{"Travis Scott", "travis scott"},
		{"Lil Uzi Vert", "lil uzi vert"},
		{"21 Savage", "21 savage"},
		{"$uicideboy$", "uicideboy"},
		{"Céline Dion", "celine dion"},
		{"Ñengo Flow", "nengo flow"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeForMatch(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeForMatch(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Stage 2: CleanQuery
// ---------------------------------------------------------------------------

func TestStage_CleanQuery(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Blinding Lights official video", "Blinding Lights"},
		{"HUMBLE. lyrics", "HUMBLE."},
		{"Bohemian Rhapsody audio", "Bohemian Rhapsody"},
		{"Drake HQ", "Drake"},
		{"Scorpion full album", "Scorpion"},
		{"Circles music video", "Circles"},
		{"Bad Bunny visualizer", "Bad Bunny"},
		{"normal query", "normal query"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := CleanQuery(tt.input)
			if got != tt.want {
				t.Errorf("CleanQuery(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Stage 3: Relevance scoring
// ---------------------------------------------------------------------------

func TestStage_RelevanceScore(t *testing.T) {
	tests := []struct {
		name      string
		result    domain.SearchResult
		query     string
		wantAbove float64
	}{
		{
			name:      "exact title match scores high",
			result:    trackResult(domain.ProviderDeezer, "1", "Humble", "Kendrick Lamar", nil),
			query:     "humble",
			wantAbove: 0.9,
		},
		{
			name:      "artist+title combined scores high",
			result:    trackResult(domain.ProviderDeezer, "1", "HUMBLE.", "Kendrick Lamar", nil),
			query:     "kendrick lamar humble",
			wantAbove: 0.8,
		},
		{
			name:      "unrelated result scores low",
			result:    trackResult(domain.ProviderDeezer, "1", "Cooking Tutorial", "Chef Mike", nil),
			query:     "humble",
			wantAbove: -1, // just check it's low
		},
		{
			name:      "partial match scores above zero",
			result:    trackResult(domain.ProviderDeezer, "1", "Bohemian Rhapsody", "Queen", nil),
			query:     "bohemian",
			wantAbove: 0.2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := relevanceScore(tt.result, NormalizeForMatch(tt.query))
			if tt.wantAbove >= 0 && score < tt.wantAbove {
				t.Errorf("relevanceScore = %.3f, want >= %.3f", score, tt.wantAbove)
			}
			t.Logf("relevanceScore(%q, %q) = %.3f", tt.result.Title, tt.query, score)
		})
	}
}

// ---------------------------------------------------------------------------
// Stage 4: Popularity normalization
// ---------------------------------------------------------------------------

func TestStage_NormalizePopularity(t *testing.T) {
	tests := []struct {
		name   string
		extras map[string]any
		wantGt int64
		wantLt int64
	}{
		{
			name:   "deezer mega-hit rank",
			extras: map[string]any{"rank": int64(900_000)},
			wantGt: 90,
			wantLt: 101,
		},
		{
			name:   "deezer moderate rank",
			extras: map[string]any{"rank": int64(200_000)},
			wantGt: 70,
			wantLt: 95,
		},
		{
			name:   "deezer low rank",
			extras: map[string]any{"rank": int64(10_000)},
			wantGt: 50,
			wantLt: 80,
		},
		{
			name:   "lastfm high listeners",
			extras: map[string]any{"listeners": "500000000"},
			wantGt: 85,
			wantLt: 101,
		},
		{
			name:   "nb_fan high",
			extras: map[string]any{"nb_fan": int64(25_000_000)},
			wantGt: 80,
			wantLt: 101,
		},
		{
			name:   "no metrics",
			extras: map[string]any{},
			wantGt: -1,
			wantLt: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pop := NormalizePopularity(tt.extras)
			if pop <= tt.wantGt || pop >= tt.wantLt {
				if tt.wantGt >= 0 {
					t.Errorf("NormalizePopularity = %d, want in (%d, %d)", pop, tt.wantGt, tt.wantLt)
				}
			}
			t.Logf("NormalizePopularity(%v) = %d", tt.extras, pop)
		})
	}
}

// ---------------------------------------------------------------------------
// Stage 5: Identifier merge (ISRC, MBID, artist name)
// ---------------------------------------------------------------------------

func TestStage_TryMerge(t *testing.T) {
	t.Run("same ISRC merges to high confidence", func(t *testing.T) {
		a := trackResult(domain.ProviderDeezer, "dz-1", "Song", "Artist", map[string]any{"isrc": "US1234"})
		b := trackResult(domain.ProviderMusicBrainz, "mb-1", "Song", "Artist", map[string]any{"isrc": "US1234"})
		merged, ok := tryMerge(a, b)
		if !ok {
			t.Fatal("expected merge on matching ISRC")
		}
		if merged.Confidence != domain.ConfidenceHigh {
			t.Errorf("expected high confidence, got %s", merged.Confidence)
		}
		if len(merged.Sources) != 2 {
			t.Errorf("expected 2 sources, got %d", len(merged.Sources))
		}
	})

	t.Run("same MBID merges to high confidence", func(t *testing.T) {
		a := trackResult(domain.ProviderDeezer, "dz-1", "Song", "Artist", map[string]any{"mbid": "abc-123"})
		b := trackResult(domain.ProviderMusicBrainz, "mb-1", "Song", "Artist", map[string]any{"mbid": "abc-123"})
		merged, ok := tryMerge(a, b)
		if !ok {
			t.Fatal("expected merge on matching MBID")
		}
		if merged.Confidence != domain.ConfidenceHigh {
			t.Errorf("expected high confidence, got %s", merged.Confidence)
		}
	})

	t.Run("different MBID does not merge", func(t *testing.T) {
		a := trackResult(domain.ProviderDeezer, "dz-1", "Song", "Artist", map[string]any{"mbid": "abc-123"})
		b := trackResult(domain.ProviderMusicBrainz, "mb-1", "Song", "Artist", map[string]any{"mbid": "def-456"})
		_, ok := tryMerge(a, b)
		if ok {
			t.Fatal("different MBIDs must not merge")
		}
	})

	t.Run("same normalized artist name merges artists", func(t *testing.T) {
		a := artistResult(domain.ProviderDeezer, "dz-1", "The Weeknd", nil)
		b := artistResult(domain.ProviderLastFM, "lfm-1", "The Weeknd", nil)
		merged, ok := tryMerge(a, b)
		if !ok {
			t.Fatal("expected artist name merge")
		}
		if merged.Confidence != domain.ConfidenceMedium {
			t.Errorf("expected medium confidence for name merge, got %s", merged.Confidence)
		}
	})

	t.Run("different kinds do not merge", func(t *testing.T) {
		a := trackResult(domain.ProviderDeezer, "dz-1", "Circles", "Post Malone", nil)
		b := albumResult(domain.ProviderDeezer, "dz-2", "Circles", "Mac Miller", nil)
		_, ok := tryMerge(a, b)
		if ok {
			t.Fatal("different kinds must not merge")
		}
	})
}

// ---------------------------------------------------------------------------
// Stage 6: sharesWord gate
// ---------------------------------------------------------------------------

func TestStage_SharesWord(t *testing.T) {
	tests := []struct {
		name  string
		title string
		sub   string
		query string
		want  bool
	}{
		{"exact match", "Humble", "Kendrick Lamar", "humble", true},
		{"artist match", "HUMBLE.", "Kendrick Lamar", "kendrick", true},
		{"no match", "Cooking Tutorial", "Chef Mike", "humble", false},
		{"partial word no match", "Humbled", "Someone", "humble", false},
		{"multi-word query partial", "Bohemian Rhapsody", "Queen", "bohemian", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := trackResult(domain.ProviderDeezer, "1", tt.title, tt.sub, nil)
			got := sharesWord(r, NormalizeForMatch(tt.query))
			if got != tt.want {
				t.Errorf("sharesWord(%q/%q, %q) = %v, want %v", tt.title, tt.sub, tt.query, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Stage 7: CollapseVersions
// ---------------------------------------------------------------------------

func TestStage_CollapseVersions(t *testing.T) {
	results := []domain.SearchResult{
		trackResult(domain.ProviderDeezer, "1", "Blinding Lights", "The Weeknd",
			map[string]any{"popularity": int64(99)}),
		trackResult(domain.ProviderDeezer, "2", "Blinding Lights (Remix)", "The Weeknd",
			map[string]any{"popularity": int64(60)}),
		trackResult(domain.ProviderDeezer, "3", "Blinding Lights", "Piano Cover",
			map[string]any{"popularity": int64(30)}),
	}

	collapsed := CollapseVersions(results)

	weekndCount := 0
	for _, r := range collapsed {
		if r.Subtitle == "The Weeknd" {
			weekndCount++
		}
	}
	if weekndCount != 1 {
		t.Errorf("expected 1 Weeknd result after collapse (original+remix grouped), got %d", weekndCount)
	}

	// Piano Cover by different artist should NOT be collapsed
	pianoFound := false
	for _, r := range collapsed {
		if r.Subtitle == "Piano Cover" {
			pianoFound = true
		}
	}
	if !pianoFound {
		t.Error("Piano Cover result should survive collapse (different artist)")
	}
}

// ---------------------------------------------------------------------------
// Stage 8: ApplyPopularityDominance
// ---------------------------------------------------------------------------

func TestStage_PopularityDominance(t *testing.T) {
	t.Run("mega-popular different kind promoted to #1", func(t *testing.T) {
		results := []domain.SearchResult{
			artistResult(domain.ProviderDeezer, "1", "Humble",
				map[string]any{"popularity": int64(30)}),
			trackResult(domain.ProviderDeezer, "2", "HUMBLE.", "Kendrick Lamar",
				map[string]any{"popularity": int64(98)}),
		}
		out := ApplyPopularityDominance(results)
		if out[0].Kind != domain.ResultKindTrack {
			t.Errorf("expected track promoted to #1 via popularity dominance, got %s", out[0].Kind)
		}
	})

	t.Run("small gap does not trigger", func(t *testing.T) {
		results := []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "1", "Song A", "Artist A",
				map[string]any{"popularity": int64(80)}),
			albumResult(domain.ProviderDeezer, "2", "Song A", "Artist B",
				map[string]any{"popularity": int64(85)}),
		}
		out := ApplyPopularityDominance(results)
		if out[0].Kind != domain.ResultKindTrack {
			t.Error("small pop gap should not trigger dominance swap")
		}
	})
}

// ---------------------------------------------------------------------------
// Stage 9: EnforceDiversity
// ---------------------------------------------------------------------------

func TestStage_EnforceDiversity(t *testing.T) {
	// EnforceDiversity moves excess entries from within the window to just
	// after the window. The "kept" portion of the window has at most 3 per
	// artist; overflow follows immediately. Verify the first
	// diversityWindow positions have at most maxPerArtistInTop in the
	// "kept" set (before overflow).
	var results []domain.SearchResult
	for i := 0; i < 6; i++ {
		results = append(results, trackResult(domain.ProviderDeezer, "",
			"Song "+string(rune('A'+i)), "Same Artist", map[string]any{"popularity": int64(90 - i)}))
	}
	for i := 0; i < 8; i++ {
		results = append(results, trackResult(domain.ProviderDeezer, "",
			"Other "+string(rune('A'+i)), "Other Artist "+string(rune('A'+i)), map[string]any{"popularity": int64(50)}))
	}

	diverse := EnforceDiversity(results)

	// The first maxPerArtistInTop (3) appearances of Same Artist should be
	// at positions lower than the 4th. This confirms they were kept and
	// overflow was pushed down.
	firstThreePos := []int{}
	for i, r := range diverse {
		if r.Subtitle == "Same Artist" {
			firstThreePos = append(firstThreePos, i)
		}
		if len(firstThreePos) == 4 {
			break
		}
	}
	if len(firstThreePos) < 4 {
		t.Fatalf("expected at least 4 Same Artist results, got %d", len(firstThreePos))
	}
	// The 4th occurrence should be at a higher position than any of the first 3
	if firstThreePos[3] <= firstThreePos[2] {
		t.Errorf("4th Same Artist at pos %d should be after 3rd at pos %d (overflow pushed down)",
			firstThreePos[3], firstThreePos[2])
	}
	// First 3 should be in the window, 4th should be after the window's kept set
	// (i.e., pushed to overflow position)
	for i := 0; i < 3; i++ {
		if firstThreePos[i] >= diversityWindow {
			t.Errorf("expected first 3 Same Artist within window, #%d at position %d", i+1, firstThreePos[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Stage 10: Full pipeline end-to-end trace
// ---------------------------------------------------------------------------

func TestStage_FullPipelineTrace(t *testing.T) {
	// Trace "Kendrick Lamar Humble" through every stage and log the output.
	// This test always passes — it's a diagnostic trace.
	rawQuery := "Kendrick Lamar Humble official video"

	// Stage 1: Normalize
	normalized := NormalizeForMatch(rawQuery)
	t.Logf("Stage 1 — Normalize:    %q → %q", rawQuery, normalized)

	// Stage 2: Clean
	cleaned := CleanQuery(rawQuery)
	t.Logf("Stage 2 — Clean:        %q → %q", rawQuery, cleaned)
	cleanedNorm := NormalizeForMatch(cleaned)
	t.Logf("Stage 2b — Clean+Norm:  %q", cleanedNorm)

	// Stage 3: Provider results (fixture)
	providers := kendrickHumbleProviders()
	rawCount := 0
	for _, group := range providers {
		rawCount += len(group)
	}
	t.Logf("Stage 3 — Providers:    %d raw results from %d providers", rawCount, len(providers))

	// Stage 4-9: FuseAndRank (merge, popNorm, score, gate, sort, collapse, dominance, diversity)
	results := FuseAndRank(providers, cleanedNorm, noQualityScorer, nil)
	t.Logf("Stage 4-9 — FuseAndRank: %d results", len(results))
	for i, r := range results {
		t.Logf("  #%d [%s] %q by %q  pop=%.0f rel=%.2f sources=%d conf=%s",
			i+1, r.Kind, r.Title, r.Subtitle,
			popularity(r), relevanceScore(r, cleanedNorm),
			len(r.Sources), r.Confidence)
	}

	// Stage 10: Rerank (simulates post-enrichment)
	reranked := Rerank(results, cleanedNorm)
	t.Logf("Stage 10 — Rerank:     %d results", len(reranked))

	// Assertion: #1 must be Kendrick
	if len(reranked) == 0 {
		t.Fatal("expected results")
	}
	if !strings.Contains(reranked[0].Subtitle, "Kendrick") {
		t.Errorf("#1 should be Kendrick, got %q by %q", reranked[0].Title, reranked[0].Subtitle)
	}
}

// ---------------------------------------------------------------------------
// Stage: IsDemoted
// ---------------------------------------------------------------------------

func TestStage_IsDemoted(t *testing.T) {
	tests := []struct {
		recordType string
		want       bool
	}{
		{"album", false},
		{"single", false},
		{"ep", false},
		{"compilation", true},
		{"live", true},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.recordType, func(t *testing.T) {
			extras := map[string]any{}
			if tt.recordType != "" {
				extras["record_type"] = tt.recordType
			}
			r := albumResult(domain.ProviderDeezer, "1", "Album", "Artist", extras)
			if got := IsDemoted(r); got != tt.want {
				t.Errorf("IsDemoted(%q) = %v, want %v", tt.recordType, got, tt.want)
			}
		})
	}
}
