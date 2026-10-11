package service

import (
	"slices"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func trackResult(provider domain.ProviderName, extID, title, subtitle string, extras map[string]any) domain.SearchResult {
	return domain.SearchResult{
		Kind:       domain.ResultKindTrack,
		Title:      title,
		Subtitle:   subtitle,
		Sources:    []domain.SourceRef{{Provider: provider, ExternalID: extID, URL: "https://example.com/" + extID}},
		Popularity: popFromExtras(extras),
		Extras:     extras,
	}
}

func artistResult(provider domain.ProviderName, extID, name string, extras map[string]any) domain.SearchResult {
	return domain.SearchResult{
		Kind:       domain.ResultKindArtist,
		Title:      name,
		Sources:    []domain.SourceRef{{Provider: provider, ExternalID: extID, URL: "https://example.com/" + extID}},
		Popularity: popFromExtras(extras),
		Extras:     extras,
	}
}

func albumResult(provider domain.ProviderName, extID, title, subtitle string, extras map[string]any) domain.SearchResult {
	return domain.SearchResult{
		Kind:       domain.ResultKindAlbum,
		Title:      title,
		Subtitle:   subtitle,
		Sources:    []domain.SourceRef{{Provider: provider, ExternalID: extID, URL: "https://example.com/" + extID}},
		Popularity: popFromExtras(extras),
		Extras:     extras,
	}
}

func TestEnforceDiversity(t *testing.T) {
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

	t.Run("ambiguous MBID-less artists sharing an external id stay apart by provider", func(t *testing.T) {
		results := []domain.SearchResult{
			withMBID(artistResult(domain.ProviderMusicBrainz, "mb-1", "Che", nil), "mbid-1"),
			withMBID(artistResult(domain.ProviderMusicBrainz, "mb-2", "Che", nil), "mbid-2"),
			artistResult(domain.ProviderDeezer, "1", "Che", nil),
			artistResult(domain.ProviderITunes, "1", "Che", nil),
			artistResult(domain.ProviderDeezer, "1", "Che", nil),
		}
		got := CollapseArtistDuplicates(results)
		if len(got) != 4 {
			t.Errorf("expected 4 cards (2 MBIDs, deezer:1, itunes:1), got %d", len(got))
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

	t.Run("ambiguous name keeps MBID-less entities as separate cards", func(t *testing.T) {
		mb1 := withMBID(artistResult(domain.ProviderMusicBrainz, "mb-1", "Che", nil), "mbid-1")
		mb2 := withMBID(artistResult(domain.ProviderMusicBrainz, "mb-2", "Che", nil), "mbid-2")
		results := []domain.SearchResult{
			mb1,
			mb2,
			artistResult(domain.ProviderDeezer, "dz-1", "Che", nil),
			artistResult(domain.ProviderITunes, "it-1", "Che", nil),
		}
		got := CollapseArtistDuplicates(results)
		if len(got) != 4 {
			t.Fatalf("expected all 4 ambiguous-name identities kept as separate cards, got %d", len(got))
		}
		for _, r := range got {
			if _, ok := r.Extras["collapsed_artists"]; ok {
				t.Errorf("unexpected collapsed_artists under ambiguous name (MBID=%q)", r.MBID)
			}
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

func withFans(r domain.SearchResult, fans int64) domain.SearchResult {
	r.FanCount = fans
	return r
}

func TestDropCollabArtistCards(t *testing.T) {
	yachty := withFans(artistResult(domain.ProviderDeezer, "1", "Lil Yachty", nil), 5_000_000)
	cochise := withFans(artistResult(domain.ProviderDeezer, "2", "Cochise", nil), 400_000)
	simon := withFans(artistResult(domain.ProviderDeezer, "3", "Simon", nil), 100)

	tests := []struct {
		name  string
		card  domain.SearchResult
		with  []domain.SearchResult
		keeps bool
	}{
		{"comma collab", withFans(artistResult(domain.ProviderDeezer, "10", "DDG, Lil Yachty", nil), 0), []domain.SearchResult{yachty}, false},
		{"x collab", withFans(artistResult(domain.ProviderDeezer, "11", "BIA x Lil Yachty", nil), 11), []domain.SearchResult{yachty}, false},
		{"ampersand collab", withFans(artistResult(domain.ProviderDeezer, "12", "Cochise & $NOT", nil), 1233), []domain.SearchResult{cochise}, false},
		{"feat collab", withFans(artistResult(domain.ProviderDeezer, "13", "Lil Yachty Feat. ian", nil), 0), []domain.SearchResult{yachty}, false},
		{"a real duo bigger than its namesake", withFans(artistResult(domain.ProviderDeezer, "14", "Simon & Garfunkel", nil), 2_000_000), []domain.SearchResult{simon}, true},
		{"a collab with none of its artists in the results", withFans(artistResult(domain.ProviderDeezer, "15", "DDG, Lil Yachty", nil), 0), nil, true},
		{"a name with a comma in it", withFans(artistResult(domain.ProviderDeezer, "16", "Tyler, The Creator", nil), 9_000_000), nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DropCollabArtistCards(append(append([]domain.SearchResult{}, tt.with...), tt.card))
			kept := false
			for _, r := range got {
				kept = kept || r.Title == tt.card.Title
			}
			if kept != tt.keeps {
				t.Errorf("%q kept = %v, want %v", tt.card.Title, kept, tt.keeps)
			}
			for _, solo := range tt.with {
				if !slices.Contains(titles(got), solo.Title) {
					t.Errorf("dropped the solo artist %q: %v", solo.Title, titles(got))
				}
			}
		})
	}
}

func TestDropCollabArtistCards_LeavesSongsWithCollabCreditsAlone(t *testing.T) {
	yachty := withFans(artistResult(domain.ProviderDeezer, "1", "Lil Yachty", nil), 5_000_000)
	song := trackResult(domain.ProviderDeezer, "20", "POP IT", "DDG, Lil Yachty", nil)

	if got := DropCollabArtistCards([]domain.SearchResult{yachty, song}); len(got) != 2 {
		t.Fatalf("got %v, want the song kept", titles(got))
	}
}

func TestReshape_DropsCollabArtistCards(t *testing.T) {
	yachty := withFans(artistResult(domain.ProviderDeezer, "1", "Lil Yachty", nil), 5_000_000)
	collab := artistResult(domain.ProviderDeezer, "10", "DDG, Lil Yachty", nil)

	if got := Reshape([]domain.SearchResult{yachty, collab}); len(got) != 1 {
		t.Fatalf("got %v, want only Lil Yachty", titles(got))
	}
}

func TestDropCollabArtistCards_KeepsArtistsAnotherProviderConfirms(t *testing.T) {
	fire := withFans(artistResult(domain.ProviderDeezer, "1", "Fire", nil), 9_000_000)
	band := withFans(artistResult(domain.ProviderDeezer, "2", "Earth, Wind & Fire", nil), 3_000_000)
	band.Sources = append(band.Sources, domain.SourceRef{Provider: domain.ProviderSpotify, ExternalID: "sp"})

	got := DropCollabArtistCards([]domain.SearchResult{fire, band})

	if !slices.Equal(titles(got), []string{"Fire", "Earth, Wind & Fire"}) {
		t.Fatalf("got %v, want the band Spotify also lists kept", titles(got))
	}
}

func TestDropCollabArtistCards_KeepsTheTopPick(t *testing.T) {
	yachty := withFans(artistResult(domain.ProviderDeezer, "1", "Lil Yachty", nil), 5_000_000)
	picked := artistResult(domain.ProviderDeezer, "2", "Simon & Garfunkel", map[string]any{domain.ExtraTopPick: true})
	simon := withFans(artistResult(domain.ProviderDeezer, "3", "Simon", nil), 9_000_000)

	got := DropCollabArtistCards([]domain.SearchResult{yachty, simon, picked})

	if !slices.Contains(titles(got), "Simon & Garfunkel") {
		t.Fatalf("got %v, want the top pick kept", titles(got))
	}
}
