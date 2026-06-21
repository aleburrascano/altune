package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// Shared result-builder helpers, relocated here when the v1 ranking tests were
// removed. Still used by the surviving find_related tests and the diversity
// tests below.

// trackResult builds a track result with a Deezer source (passes
// hasBrowseableSource for all kinds).
func trackResult(provider domain.ProviderName, extID, title, subtitle string, extras map[string]any) domain.SearchResult {
	return domain.SearchResult{
		Kind:     domain.ResultKindTrack,
		Title:    title,
		Subtitle: subtitle,
		Sources:  []domain.SourceRef{{Provider: provider, ExternalID: extID, URL: "https://example.com/" + extID}},
		Extras:   extras,
	}
}

// artistResult builds an artist result. Non-track results need a Deezer source
// to pass hasBrowseableSource.
func artistResult(provider domain.ProviderName, extID, name string, extras map[string]any) domain.SearchResult {
	return domain.SearchResult{
		Kind:    domain.ResultKindArtist,
		Title:   name,
		Sources: []domain.SourceRef{{Provider: provider, ExternalID: extID, URL: "https://example.com/" + extID}},
		Extras:  extras,
	}
}

// albumResult builds an album result.
func albumResult(provider domain.ProviderName, extID, title, subtitle string, extras map[string]any) domain.SearchResult {
	return domain.SearchResult{
		Kind:     domain.ResultKindAlbum,
		Title:    title,
		Subtitle: subtitle,
		Sources:  []domain.SourceRef{{Provider: provider, ExternalID: extID, URL: "https://example.com/" + extID}},
		Extras:   extras,
	}
}

func TestEnforceDiversity(t *testing.T) {
	// EnforceDiversity moves excess entries from within the window to just
	// after the window. The "kept" portion of the window has at most 3 per
	// artist; overflow follows immediately.
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

	firstFourPos := []int{}
	for i, r := range diverse {
		if r.Subtitle == "Same Artist" {
			firstFourPos = append(firstFourPos, i)
		}
		if len(firstFourPos) == 4 {
			break
		}
	}
	if len(firstFourPos) < 4 {
		t.Fatalf("expected at least 4 Same Artist results, got %d", len(firstFourPos))
	}
	if firstFourPos[3] <= firstFourPos[2] {
		t.Errorf("4th Same Artist at pos %d should be after 3rd at pos %d (overflow pushed down)",
			firstFourPos[3], firstFourPos[2])
	}
	for i := 0; i < 3; i++ {
		if firstFourPos[i] >= diversityWindow {
			t.Errorf("expected first 3 Same Artist within window, #%d at position %d", i+1, firstFourPos[i])
		}
	}
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
