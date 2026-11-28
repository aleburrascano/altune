package service_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service"
	"slices"
	"testing"
)

const diversityWindow = 10

func TestEnforceDiversity(t *testing.T) {
	var results []domain.SearchResult
	for i := 0; i < 6; i++ {
		results = append(results, memory.TrackResult(domain.ProviderDeezer, "",
			"Song "+string(rune('A'+i)), "Same Artist", map[string]any{"popularity": int64(90 - i)}))
	}
	for i := 0; i < 8; i++ {
		results = append(results, memory.TrackResult(domain.ProviderDeezer, "",
			"Other "+string(rune('A'+i)), "Other Artist "+string(rune('A'+i)), map[string]any{"popularity": int64(50)}))
	}

	diverse := service.EnforceDiversity(results)

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
			memory.ArtistResult(domain.ProviderDeezer, "1", "Che", map[string]any{
				"popularity": float64(80), "disambiguation": "Atlanta rapper",
			}),
			memory.TrackResult(domain.ProviderDeezer, "t1", "BA$$", "Che", nil),
			memory.ArtistResult(domain.ProviderDeezer, "2", "Che", map[string]any{
				"popularity": float64(30), "disambiguation": "Korean singer-songwriter",
			}),
			memory.ArtistResult(domain.ProviderDeezer, "3", "Che", map[string]any{
				"popularity": float64(10),
			}),
		}
		got := service.CollapseArtistDuplicates(results)

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
		list, ok := collapsed.([]domain.CollapsedArtistSummary)
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
			memory.ArtistResult(domain.ProviderDeezer, "1", "Drake", map[string]any{"popularity": float64(90)}),
			memory.ArtistResult(domain.ProviderDeezer, "2", "Aurora", map[string]any{"popularity": float64(70)}),
		}
		got := service.CollapseArtistDuplicates(results)
		if len(got) != 2 {
			t.Errorf("expected 2 distinct artists preserved, got %d", len(got))
		}
		if _, ok := got[0].Extras["collapsed_artists"]; ok {
			t.Error("unexpected collapsed_artists on unique-name artist")
		}
	})

	t.Run("single artist not collapsed", func(t *testing.T) {
		results := []domain.SearchResult{
			memory.ArtistResult(domain.ProviderDeezer, "1", "Che", map[string]any{"popularity": float64(80)}),
		}
		got := service.CollapseArtistDuplicates(results)
		if len(got) != 1 {
			t.Errorf("expected 1 result, got %d", len(got))
		}
		if _, ok := got[0].Extras["collapsed_artists"]; ok {
			t.Error("single artist should not have collapsed_artists")
		}
	})
}

func withFans(r domain.SearchResult, fans int64) domain.SearchResult {
	r.FanCount = fans
	return r
}

func TestDropCollabArtistCards(t *testing.T) {
	yachty := withFans(memory.ArtistResult(domain.ProviderDeezer, "1", "Lil Yachty", nil), 5_000_000)
	cochise := withFans(memory.ArtistResult(domain.ProviderDeezer, "2", "Cochise", nil), 400_000)
	simon := withFans(memory.ArtistResult(domain.ProviderDeezer, "3", "Simon", nil), 100)

	tests := []struct {
		name  string
		card  domain.SearchResult
		with  []domain.SearchResult
		keeps bool
	}{
		{"comma collab", withFans(memory.ArtistResult(domain.ProviderDeezer, "10", "DDG, Lil Yachty", nil), 0), []domain.SearchResult{yachty}, false},
		{"x collab", withFans(memory.ArtistResult(domain.ProviderDeezer, "11", "BIA x Lil Yachty", nil), 11), []domain.SearchResult{yachty}, false},
		{"ampersand collab", withFans(memory.ArtistResult(domain.ProviderDeezer, "12", "Cochise & $NOT", nil), 1233), []domain.SearchResult{cochise}, false},
		{"feat collab", withFans(memory.ArtistResult(domain.ProviderDeezer, "13", "Lil Yachty Feat. ian", nil), 0), []domain.SearchResult{yachty}, false},
		{"a real duo bigger than its namesake", withFans(memory.ArtistResult(domain.ProviderDeezer, "14", "Simon & Garfunkel", nil), 2_000_000), []domain.SearchResult{simon}, true},
		{"a collab with none of its artists in the results", withFans(memory.ArtistResult(domain.ProviderDeezer, "15", "DDG, Lil Yachty", nil), 0), nil, true},
		{"a name with a comma in it", withFans(memory.ArtistResult(domain.ProviderDeezer, "16", "Tyler, The Creator", nil), 9_000_000), nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := service.DropCollabArtistCards(append(append([]domain.SearchResult{}, tt.with...), tt.card))
			kept := false
			for _, r := range got {
				kept = kept || r.Title == tt.card.Title
			}
			if kept != tt.keeps {
				t.Errorf("%q kept = %v, want %v", tt.card.Title, kept, tt.keeps)
			}
			for _, solo := range tt.with {
				if !slices.Contains(memory.Titles(got), solo.Title) {
					t.Errorf("dropped the solo artist %q: %v", solo.Title, memory.Titles(got))
				}
			}
		})
	}
}

func TestDropCollabArtistCards_LeavesSongsWithCollabCreditsAlone(t *testing.T) {
	yachty := withFans(memory.ArtistResult(domain.ProviderDeezer, "1", "Lil Yachty", nil), 5_000_000)
	song := memory.TrackResult(domain.ProviderDeezer, "20", "POP IT", "DDG, Lil Yachty", nil)

	if got := service.DropCollabArtistCards([]domain.SearchResult{yachty, song}); len(got) != 2 {
		t.Fatalf("got %v, want the song kept", memory.Titles(got))
	}
}

func TestReshape_DropsCollabArtistCards(t *testing.T) {
	yachty := withFans(memory.ArtistResult(domain.ProviderDeezer, "1", "Lil Yachty", nil), 5_000_000)
	collab := memory.ArtistResult(domain.ProviderDeezer, "10", "DDG, Lil Yachty", nil)

	if got := service.Reshape([]domain.SearchResult{yachty, collab}); len(got) != 1 {
		t.Fatalf("got %v, want only Lil Yachty", memory.Titles(got))
	}
}

func TestDropCollabArtistCards_KeepsArtistsAnotherProviderConfirms(t *testing.T) {
	fire := withFans(memory.ArtistResult(domain.ProviderDeezer, "1", "Fire", nil), 9_000_000)
	band := withFans(memory.ArtistResult(domain.ProviderDeezer, "2", "Earth, Wind & Fire", nil), 3_000_000)
	band.Sources = append(band.Sources, domain.ProviderLink{Provider: domain.ProviderSpotify, ExternalID: "sp"})

	got := service.DropCollabArtistCards([]domain.SearchResult{fire, band})

	if !slices.Equal(memory.Titles(got), []string{"Fire", "Earth, Wind & Fire"}) {
		t.Fatalf("got %v, want the band Spotify also lists kept", memory.Titles(got))
	}
}

func TestDropCollabArtistCards_KeepsTheTopPick(t *testing.T) {
	yachty := withFans(memory.ArtistResult(domain.ProviderDeezer, "1", "Lil Yachty", nil), 5_000_000)
	picked := memory.ArtistResult(domain.ProviderDeezer, "2", "Simon & Garfunkel", map[string]any{domain.ExtraTopPick: true})
	simon := withFans(memory.ArtistResult(domain.ProviderDeezer, "3", "Simon", nil), 9_000_000)

	got := service.DropCollabArtistCards([]domain.SearchResult{yachty, simon, picked})

	if !slices.Contains(memory.Titles(got), "Simon & Garfunkel") {
		t.Fatalf("got %v, want the top pick kept", memory.Titles(got))
	}
}
