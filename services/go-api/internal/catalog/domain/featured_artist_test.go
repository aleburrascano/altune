package domain

import "testing"

func TestNewFeaturedArtist(t *testing.T) {
	t.Run("trims and defaults role", func(t *testing.T) {
		fa, ok := NewFeaturedArtist("  SZA ", " mb-1 ", 42)
		if !ok {
			t.Fatal("expected ok")
		}
		if fa.Name != "SZA" || fa.MBID != "mb-1" || fa.DeezerID != 42 || fa.Role != RoleFeatured {
			t.Errorf("got %+v", fa)
		}
	})
	t.Run("empty name rejected", func(t *testing.T) {
		if _, ok := NewFeaturedArtist("   ", "", 0); ok {
			t.Error("expected empty name to be rejected")
		}
	})
}

func TestFeaturedArtist_IdentityKey(t *testing.T) {
	tests := []struct {
		name string
		fa   FeaturedArtist
		want string
	}{
		{"mbid raw (matches generated col)", FeaturedArtist{Name: "X", MBID: "abc", DeezerID: 9}, "abc"},
		{"deezer id", FeaturedArtist{Name: "X", DeezerID: 9}, "dz:9"},
		{"name", FeaturedArtist{Name: "Kendrick  Lamar"}, "name:kendrick lamar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fa.IdentityKey(); got != tt.want {
				t.Errorf("IdentityKey() = %q, want %q", got, tt.want)
			}
		})
	}
}
