package domain

import "testing"

func TestFeaturedArtist_IdentityKey(t *testing.T) {
	tests := []struct {
		name string
		fa   FeaturedArtist
		want string
	}{
		{"mbid wins", FeaturedArtist{Name: "X", MBID: "abc", DeezerID: 9}, "mb:abc"},
		{"deezer id next", FeaturedArtist{Name: "X", DeezerID: 9}, "dz:9"},
		{"name last", FeaturedArtist{Name: "  Kendrick  Lamar "}, "name:kendrick lamar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fa.IdentityKey(); got != tt.want {
				t.Errorf("IdentityKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFeaturedArtist_ExtrasRoundTrip(t *testing.T) {
	in := FeaturedArtist{Name: "SZA", MBID: "mb-1", DeezerID: 42, Role: RoleFeatured}
	got := FeaturedArtistFromMap(in.ToExtrasMap())
	if got != in {
		t.Errorf("round trip = %+v, want %+v", got, in)
	}
}

func TestFeaturedArtist_FromMap_JSONNumeric(t *testing.T) {
	// JSON round-trips numbers as float64 — the parser must tolerate it.
	m := map[string]any{"name": "SZA", "deezer_id": float64(42)}
	got := FeaturedArtistFromMap(m)
	if got.DeezerID != 42 {
		t.Errorf("DeezerID = %d, want 42", got.DeezerID)
	}
	if got.Role != RoleFeatured {
		t.Errorf("Role = %q, want %q (defaulted)", got.Role, RoleFeatured)
	}
}

func TestFeaturedArtist_ToExtrasMap_OmitsEmptyIDs(t *testing.T) {
	m := FeaturedArtist{Name: "X", Role: RoleFeatured}.ToExtrasMap()
	if _, ok := m["mbid"]; ok {
		t.Error("empty mbid should be omitted")
	}
	if _, ok := m["deezer_id"]; ok {
		t.Error("zero deezer_id should be omitted")
	}
}

func TestFeaturedArtistsToExtras_EmptyIsNil(t *testing.T) {
	if got := FeaturedArtistsToExtras(nil); got != nil {
		t.Errorf("empty slice should map to nil, got %v", got)
	}
}

func TestFeaturedArtistsToExtras_NonEmpty(t *testing.T) {
	fs := []FeaturedArtist{
		{Name: "SZA", MBID: "mb-1", Role: RoleFeatured},
		{Name: "Rihanna", DeezerID: 564, Role: RoleFeatured},
	}
	got := FeaturedArtistsToExtras(fs)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0]["name"] != "SZA" || got[0]["mbid"] != "mb-1" {
		t.Errorf("first entry = %v", got[0])
	}
	if got[1]["name"] != "Rihanna" || got[1]["deezer_id"] != int64(564) {
		t.Errorf("second entry = %v", got[1])
	}
}

func TestFeaturedArtistsFromExtras(t *testing.T) {
	fs := []FeaturedArtist{
		{Name: "SZA", MBID: "mb-1", Role: RoleFeatured},
		{Name: "Rihanna", DeezerID: 564, Role: RoleFeatured},
	}
	extras := map[string]any{"featured_artists": FeaturedArtistsToExtras(fs)}

	got := FeaturedArtistsFromExtras(extras)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0] != fs[0] || got[1] != fs[1] {
		t.Errorf("round-trip = %+v, want %+v", got, fs)
	}
}

func TestFeaturedArtistsFromExtras_AbsentOrWrongShape(t *testing.T) {
	tests := []struct {
		name   string
		extras map[string]any
	}{
		{"key absent", map[string]any{}},
		{"nil extras", nil},
		// A JSON round-trip decodes to []any, not []map[string]any — that shape
		// is intentionally not accepted here.
		{"json-decoded shape", map[string]any{"featured_artists": []any{map[string]any{"name": "SZA"}}}},
		{"wrong type", map[string]any{"featured_artists": "SZA"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FeaturedArtistsFromExtras(tt.extras); got != nil {
				t.Errorf("FeaturedArtistsFromExtras(%v) = %v, want nil", tt.extras, got)
			}
		})
	}
}

func TestFeaturedArtist_FromMap_NumericVariants(t *testing.T) {
	// deezer_id must parse from all three numeric shapes the untyped map can
	// carry (native int64, plain int, JSON float64).
	tests := []struct {
		name string
		id   any
	}{
		{"int64", int64(42)},
		{"int", int(42)},
		{"float64", float64(42)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FeaturedArtistFromMap(map[string]any{"name": "SZA", "deezer_id": tt.id})
			if got.DeezerID != 42 {
				t.Errorf("DeezerID = %d, want 42", got.DeezerID)
			}
		})
	}
}

func TestFeaturedArtist_FromMap_NonStringFieldsIgnored(t *testing.T) {
	got := FeaturedArtistFromMap(map[string]any{"name": 7, "mbid": true, "deezer_id": "42"})
	if got.Name != "" || got.MBID != "" || got.DeezerID != 0 {
		t.Errorf("non-string/non-numeric values must be ignored, got %+v", got)
	}
	if got.Role != RoleFeatured {
		t.Errorf("Role = %q, want defaulted %q", got.Role, RoleFeatured)
	}
}
