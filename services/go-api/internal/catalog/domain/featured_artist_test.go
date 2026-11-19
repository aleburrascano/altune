package domain_test

import (
	"altune/go-api/internal/catalog"
	"testing"
)

func TestNewFeaturedArtist(t *testing.T) {
	t.Run("trims and defaults role", func(t *testing.T) {
		fa, ok := catalog.NewFeaturedArtist("  SZA ", " mb-1 ", 42)
		if !ok {
			t.Fatal("expected ok")
		}
		if fa.Name != "SZA" || fa.MBID != "mb-1" || fa.DeezerID != 42 || fa.Role != catalog.RoleFeatured {
			t.Errorf("got %+v", fa)
		}
	})
	t.Run("empty name rejected", func(t *testing.T) {
		if _, ok := catalog.NewFeaturedArtist("   ", "", 0); ok {
			t.Error("expected empty name to be rejected")
		}
	})
}

func TestFeaturedArtist_IdentityKey(t *testing.T) {
	tests := []struct {
		name string
		fa   catalog.FeaturedArtist
		want string
	}{
		{"mbid raw (matches generated col)", catalog.FeaturedArtist{Name: "X", MBID: "abc", DeezerID: 9}, "abc"},
		{"deezer id", catalog.FeaturedArtist{Name: "X", DeezerID: 9}, "dz:9"},
		{"name", catalog.FeaturedArtist{Name: "Kendrick  Lamar"}, "name:kendrick lamar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fa.IdentityKey(); got != tt.want {
				t.Errorf("IdentityKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFeaturedArtist_IdentityKey_NFKC(t *testing.T) {
	pairs := []struct{ name, a, b string }{
		{"NFC vs NFD", "Beyoncé", "Beyoncé"},
		{"fullwidth vs ASCII", "ＲＯＳＡＬＩＡ", "Rosalia"},
		{"ligature", "ﬁve", "five"},
	}
	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			a, b := catalog.FeaturedArtist{Name: p.a}, catalog.FeaturedArtist{Name: p.b}
			if a.IdentityKey() != b.IdentityKey() {
				t.Errorf("IdentityKey %q != %q", a.IdentityKey(), b.IdentityKey())
			}
			if a.NormalizedName() != b.NormalizedName() {
				t.Errorf("NormalizedName %q != %q", a.NormalizedName(), b.NormalizedName())
			}
		})
	}
}

func TestFeaturedArtist_LegacyIdentityKey(t *testing.T) {
	tests := []struct {
		name string
		fa   catalog.FeaturedArtist
		want string
	}{
		{"mbid unchanged", catalog.FeaturedArtist{Name: "ﬁve", MBID: "abc"}, "abc"},
		{"deezer unchanged", catalog.FeaturedArtist{Name: "ﬁve", DeezerID: 9}, "dz:9"},
		{"ascii name equals current key", catalog.FeaturedArtist{Name: "Kendrick  Lamar"}, "name:kendrick lamar"},
		{"compat name keeps pre-NFKC fold", catalog.FeaturedArtist{Name: "Beyoncé"}, "name:beyoncé"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fa.LegacyIdentityKey(); got != tt.want {
				t.Errorf("LegacyIdentityKey() = %q, want %q", got, tt.want)
			}
		})
	}
}
