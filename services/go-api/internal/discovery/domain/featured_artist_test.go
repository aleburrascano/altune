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
