package domain

import "testing"

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FeaturedFromText(tt.title, tt.subtitle)

			if len(got) != len(tt.wantNames) {
				t.Fatalf("got %d credits (%+v), want %d", len(got), got, len(tt.wantNames))
			}
			for i, want := range tt.wantNames {
				if got[i].Name != want {
					t.Errorf("credit[%d] = %q, want %q", i, got[i].Name, want)
				}
				if got[i].Role != RoleFeatured {
					t.Errorf("credit[%d].Role = %q, want %q", i, got[i].Role, RoleFeatured)
				}
			}
		})
	}
}
