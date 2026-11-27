package domain_test

import (
	"altune/go-api/internal/library/internal/domain"
	"strings"
	"testing"
)

func TestValidateTrackNumber(t *testing.T) {
	t.Parallel()
	tests := []struct {
		n       int
		invalid bool
	}{
		{n: 1}, {n: domain.MaxTrackNumber}, {n: 0, invalid: true}, {n: -1, invalid: true}, {n: domain.MaxTrackNumber + 1, invalid: true},
	}
	for _, tt := range tests {
		if got := domain.ValidateTrackNumber(tt.n) != nil; got != tt.invalid {
			t.Errorf("ValidateTrackNumber(%d) refused = %v, want %v", tt.n, got, tt.invalid)
		}
	}
}

func TestValidateIdempotencyKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		key     string
		invalid bool
	}{
		{name: "ordinary", key: "k-1"},
		{name: "at the limit", key: strings.Repeat("k", domain.MaxIdempotencyKeyLength)},
		{name: "empty", key: "", invalid: true},
		{name: "over the limit", key: strings.Repeat("k", domain.MaxIdempotencyKeyLength+1), invalid: true},
		{name: "NUL byte", key: "a\x00b", invalid: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.ValidateIdempotencyKey(tt.key) != nil; got != tt.invalid {
				t.Errorf("refused = %v, want %v", got, tt.invalid)
			}
		})
	}
}

func TestValidateFeaturedArtists_RefusesMoreThanTheMaximum(t *testing.T) {
	t.Parallel()
	feats := make([]domain.FeaturedArtist, domain.MaxFeaturedArtistsPerTrack+1)
	for i := range feats {
		feats[i] = domain.FeaturedArtist{Name: "Guest"}
	}

	if domain.ValidateFeaturedArtists(feats) == nil {
		t.Error("want a validation error over the maximum count")
	}
	if err := domain.ValidateFeaturedArtists(feats[:domain.MaxFeaturedArtistsPerTrack]); err != nil {
		t.Errorf("at the maximum: %v", err)
	}
}
