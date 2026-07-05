package providers

import "testing"

func TestExtractDeezerFeatured(t *testing.T) {
	cs := []deezerContributor{
		{ID: 1, Name: "Main Artist", Role: "Main"},
		{ID: 2, Name: "Guest One", Role: "Featured"},
		{ID: 3, Name: "  ", Role: "Featured"}, // blank name skipped
		{ID: 4, Name: "Guest Two", Role: "featured"}, // case-insensitive
	}
	got := extractDeezerFeatured(cs)
	if len(got) != 2 {
		t.Fatalf("got %d featured, want 2 (%+v)", len(got), got)
	}
	if got[0].Name != "Guest One" || got[0].DeezerID != 2 {
		t.Errorf("featured[0] = %+v, want Guest One / 2", got[0])
	}
	if got[1].Name != "Guest Two" || got[1].DeezerID != 4 {
		t.Errorf("featured[1] = %+v, want Guest Two / 4", got[1])
	}
}

func TestExtractDeezerFeatured_NoneWhenAllMain(t *testing.T) {
	got := extractDeezerFeatured([]deezerContributor{{ID: 1, Name: "Solo", Role: "Main"}})
	if len(got) != 0 {
		t.Errorf("expected no featured, got %+v", got)
	}
}

func TestExtractDeezerFeatured_CoPrimaryMain(t *testing.T) {
	// Opium-style co-billing: the collaborator is credited "Main", not "Featured".
	cs := []deezerContributor{
		{ID: 1, Name: "Ken Carson", Role: "Main"}, // primary — skipped
		{ID: 2, Name: "Playboi Carti", Role: "Main"},
	}
	got := extractDeezerFeatured(cs)
	if len(got) != 1 || got[0].Name != "Playboi Carti" || got[0].DeezerID != 2 {
		t.Fatalf("expected [Playboi Carti/2], got %+v", got)
	}
}

func TestExtractDeezerFeatured_DedupesAndKeepsFeaturedAfterCoMain(t *testing.T) {
	cs := []deezerContributor{
		{ID: 1, Name: "Ken Carson", Role: "Main"},   // primary — skipped
		{ID: 2, Name: "Destroy Lonely", Role: "Main"}, // co-primary
		{ID: 3, Name: "Lil Uzi Vert", Role: "Featured"},
		{ID: 4, Name: "destroy lonely", Role: "Featured"}, // dup by name
	}
	got := extractDeezerFeatured(cs)
	if len(got) != 2 {
		t.Fatalf("expected 2 (dedup), got %+v", got)
	}
	if got[0].Name != "Destroy Lonely" || got[1].Name != "Lil Uzi Vert" {
		t.Errorf("order/dedup wrong: %+v", got)
	}
}
