package domain_test

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/jsoncontract"
	"encoding/json"
	"testing"
)

func TestWire_AFeaturedArtistIsSentAsTheExtrasMapTheClientReads(t *testing.T) {
	full := domain.FeaturedArtist{Name: "Guest", MBID: "6b1c1b3e-0000-4000-8000-000000000001", DeezerID: 42, Role: domain.RoleFeatured}

	body, err := json.Marshal(full.ToExtrasMap())
	if err != nil {
		t.Fatalf("marshal extras: %v", err)
	}

	jsoncontract.AssertWire(t, "featured_artist_extras", body)
}
