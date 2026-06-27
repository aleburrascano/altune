package cache

import (
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
)

func TestNegativeTTL_PerKind(t *testing.T) {
	tests := []struct {
		name string
		kind domain.ResultKind
		want time.Duration
	}{
		{"track churns most, rechecks soonest", domain.ResultKindTrack, 6 * time.Hour},
		{"album medium churn", domain.ResultKindAlbum, 12 * time.Hour},
		{"artist most stable", domain.ResultKindArtist, 24 * time.Hour},
		{"unknown kind defaults conservative", domain.ResultKind(0), 24 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := negativeTTL(tt.kind); got != tt.want {
				t.Errorf("negativeTTL(%v) = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
	// A negative TTL must never exceed the positive TTL (a miss should re-check
	// at least as often as a hit refreshes).
	for _, k := range []domain.ResultKind{domain.ResultKindTrack, domain.ResultKindAlbum, domain.ResultKindArtist} {
		if negativeTTL(k) >= artworkPositiveTTL {
			t.Errorf("negativeTTL(%v) must be shorter than positive TTL %v", k, artworkPositiveTTL)
		}
	}
}
