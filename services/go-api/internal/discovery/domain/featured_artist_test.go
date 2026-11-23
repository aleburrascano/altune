package domain_test

import (
	"altune/go-api/internal/discovery/domain"
	"testing"
)

func TestFeaturedArtist_IdentityKey(t *testing.T) {
	tests := []struct {
		name string
		fa   domain.FeaturedArtist
		want string
	}{
		{"mbid wins", domain.FeaturedArtist{Name: "X", MBID: "abc", DeezerID: 9}, "mb:abc"},
		{"deezer id next", domain.FeaturedArtist{Name: "X", DeezerID: 9}, "dz:9"},
		{"name last", domain.FeaturedArtist{Name: "  Kendrick  Lamar "}, "name:kendrick lamar"},
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
	in := domain.FeaturedArtist{Name: "SZA", MBID: "mb-1", DeezerID: 42, Role: domain.RoleFeatured}
	got := domain.FeaturedArtistFromMap(in.ToExtrasMap())
	if got != in {
		t.Errorf("round trip = %+v, want %+v", got, in)
	}
}

func TestFeaturedArtist_FromMap_JSONNumeric(t *testing.T) {
	m := map[string]any{"name": "SZA", "deezer_id": float64(42)}
	got := domain.FeaturedArtistFromMap(m)
	if got.DeezerID != 42 {
		t.Errorf("DeezerID = %d, want 42", got.DeezerID)
	}
	if got.Role != domain.RoleFeatured {
		t.Errorf("Role = %q, want %q (defaulted)", got.Role, domain.RoleFeatured)
	}
}

func TestFeaturedArtist_ToExtrasMap_OmitsEmptyIDs(t *testing.T) {
	m := domain.FeaturedArtist{Name: "X", Role: domain.RoleFeatured}.ToExtrasMap()
	if _, ok := m["mbid"]; ok {
		t.Error("empty mbid should be omitted")
	}
	if _, ok := m["deezer_id"]; ok {
		t.Error("zero deezer_id should be omitted")
	}
}

func TestFeaturedArtistsToExtras_EmptyIsNil(t *testing.T) {
	if got := domain.FeaturedArtistsToExtras(nil); got != nil {
		t.Errorf("empty slice should map to nil, got %v", got)
	}
}

func TestFeaturedArtistsToExtras_NonEmpty(t *testing.T) {
	fs := []domain.FeaturedArtist{
		{Name: "SZA", MBID: "mb-1", Role: domain.RoleFeatured},
		{Name: "Rihanna", DeezerID: 564, Role: domain.RoleFeatured},
	}
	got := domain.FeaturedArtistsToExtras(fs)
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
	fs := []domain.FeaturedArtist{
		{Name: "SZA", MBID: "mb-1", Role: domain.RoleFeatured},
		{Name: "Rihanna", DeezerID: 564, Role: domain.RoleFeatured},
	}
	extras := map[string]any{"featured_artists": domain.FeaturedArtistsToExtras(fs)}

	got := domain.FeaturedArtistsFromExtras(extras)
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
		{"json-decoded shape", map[string]any{"featured_artists": []any{map[string]any{"name": "SZA"}}}},
		{"wrong type", map[string]any{"featured_artists": "SZA"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domain.FeaturedArtistsFromExtras(tt.extras); got != nil {
				t.Errorf("FeaturedArtistsFromExtras(%v) = %v, want nil", tt.extras, got)
			}
		})
	}
}

func TestFeaturedArtist_FromMap_NumericVariants(t *testing.T) {
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
			got := domain.FeaturedArtistFromMap(map[string]any{"name": "SZA", "deezer_id": tt.id})
			if got.DeezerID != 42 {
				t.Errorf("DeezerID = %d, want 42", got.DeezerID)
			}
		})
	}
}

func TestFeaturedArtist_FromMap_NonStringFieldsIgnored(t *testing.T) {
	got := domain.FeaturedArtistFromMap(map[string]any{"name": 7, "mbid": true, "deezer_id": "42"})
	if got.Name != "" || got.MBID != "" || got.DeezerID != 0 {
		t.Errorf("non-string/non-numeric values must be ignored, got %+v", got)
	}
	if got.Role != domain.RoleFeatured {
		t.Errorf("Role = %q, want defaulted %q", got.Role, domain.RoleFeatured)
	}
}

func TestFeaturedFromText(t *testing.T) {
	tests := []struct {
		name      string
		title     string
		subtitle  string
		wantNames []string
	}{
		{"parenthesised feat", "Sicko Mode (feat. Drake)", "Travis Scott", []string{"Drake"}},
		{"bare ft", "Jumpman ft. Future", "Drake", []string{"Future"}},
		{"multiple guests", "Track (with Ken Carson, Playboi Carti)", "Destroy Lonely", []string{"Ken Carson", "Playboi Carti"}},
		{"falls through to subtitle", "Plain Title", "Artist feat. Guest", []string{"Guest"}},
		{"no credit", "Plain Title", "Artist", nil},
		{"with inside a word", "Left Hand Free", "Alt-J", nil},
		{"with opens a plain title", "Stay With Me", "Sam Smith", nil},
		{"with or without", "With or Without You", "U2", nil},
		{"ampersand splits", "Song (feat. Jay-Z & Kanye West)", "A", []string{"Jay-Z", "Kanye West"}},
		{"dash tail credit", "Song - with Drake", "A", []string{"Drake"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.FeaturedFromText(tt.title, tt.subtitle)

			if len(got) != len(tt.wantNames) {
				t.Fatalf("got %d credits (%+v), want %d", len(got), got, len(tt.wantNames))
			}
			for i, want := range tt.wantNames {
				if got[i].Name != want {
					t.Errorf("credit[%d] = %q, want %q", i, got[i].Name, want)
				}
				if got[i].Role != domain.RoleFeatured {
					t.Errorf("credit[%d].Role = %q, want %q", i, got[i].Role, domain.RoleFeatured)
				}
			}
		})
	}
}
