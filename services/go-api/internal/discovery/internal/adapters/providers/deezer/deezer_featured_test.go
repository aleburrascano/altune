package deezer_test

import "testing"

func TestLookupTrackFeatured(t *testing.T) {
	got := featuredFrom(t, `[
		{"id": 1, "name": "Main Artist", "role": "Main"},
		{"id": 2, "name": "Guest One", "role": "Featured"},
		{"id": 3, "name": "  ", "role": "Featured"},
		{"id": 4, "name": "Guest Two", "role": "featured"}
	]`)
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

func TestLookupTrackFeatured_NoneWhenAllMain(t *testing.T) {
	got := featuredFrom(t, `[{"id": 1, "name": "Solo", "role": "Main"}]`)
	if len(got) != 0 {
		t.Errorf("expected no featured, got %+v", got)
	}
}

func TestLookupTrackFeatured_CoPrimaryMain(t *testing.T) {
	got := featuredFrom(t, `[
		{"id": 1, "name": "Ken Carson", "role": "Main"},
		{"id": 2, "name": "Playboi Carti", "role": "Main"}
	]`)
	if len(got) != 1 || got[0].Name != "Playboi Carti" || got[0].DeezerID != 2 {
		t.Fatalf("expected [Playboi Carti/2], got %+v", got)
	}
}

func TestLookupTrackFeatured_DedupesAndKeepsFeaturedAfterCoMain(t *testing.T) {
	got := featuredFrom(t, `[
		{"id": 1, "name": "Ken Carson", "role": "Main"},
		{"id": 2, "name": "Destroy Lonely", "role": "Main"},
		{"id": 3, "name": "Lil Uzi Vert", "role": "Featured"},
		{"id": 4, "name": "destroy lonely", "role": "Featured"}
	]`)
	if len(got) != 2 {
		t.Fatalf("expected 2 (dedup), got %+v", got)
	}
	if got[0].Name != "Destroy Lonely" || got[1].Name != "Lil Uzi Vert" {
		t.Errorf("order/dedup wrong: %+v", got)
	}
}
