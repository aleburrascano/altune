package domain

import "testing"

func TestEmptyEnrichment_NonNilCollections(t *testing.T) {
	e := EmptyEnrichment()

	if e.Genres == nil || e.SecondaryTypes == nil || e.ExternalIDs == nil {
		t.Fatalf("EmptyEnrichment must have non-nil collections, got %#v", e)
	}
	if len(e.Genres) != 0 || len(e.SecondaryTypes) != 0 || len(e.ExternalIDs) != 0 {
		t.Errorf("EmptyEnrichment collections must be empty, got %#v", e)
	}
	if !e.IsZero() {
		t.Error("EmptyEnrichment must report IsZero")
	}
}

func TestMBEnrichment_IsZero(t *testing.T) {
	tests := []struct {
		name string
		e    MBEnrichment
		want bool
	}{
		{"empty", EmptyEnrichment(), true},
		{"mbid only", MBEnrichment{MBID: "abc", Genres: []string{}, SecondaryTypes: []string{}, ExternalIDs: map[string]string{}}, false},
		{"genres only", MBEnrichment{Genres: []string{"hip hop"}}, false},
		{"artwork only", MBEnrichment{ArtworkURL: "https://x/y.jpg"}, false},
		{"year only", MBEnrichment{Year: 2017}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.e.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}
