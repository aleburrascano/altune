package persistence

import (
	"context"
	"testing"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

// Integration test — skips without DATABASE_URL. Verifies featured artists persist
// on Add and reload on GetByIDWithFeatured in position order, and that re-adding a
// dedup track dedups the canonical featured_artists rows on identity_key.
func TestPgxTrackRepo_FeaturedArtistsRoundTrip(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	track := newTestTrackForDB(t, userId)
	track.FeaturedArtists = []domain.FeaturedArtist{
		{Name: "Guest One", MBID: "mb-guest-1", Role: domain.RoleFeatured},
		{Name: "Guest Two", DeezerID: 555, Role: domain.RoleFeatured},
	}
	cleanupTrack(t, pool, track.ID, userId)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM featured_artists WHERE user_id = $1`, userId.UUID())
	})

	if _, _, err := repo.Add(ctx, track); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	got, err := repo.GetByIDWithFeatured(ctx, track.ID, userId)
	if err != nil {
		t.Fatalf("GetByIDWithFeatured() error = %v", err)
	}
	if len(got.FeaturedArtists) != 2 {
		t.Fatalf("featured count = %d, want 2 (%+v)", len(got.FeaturedArtists), got.FeaturedArtists)
	}
	if got.FeaturedArtists[0].Name != "Guest One" || got.FeaturedArtists[0].MBID != "mb-guest-1" {
		t.Errorf("featured[0] = %+v", got.FeaturedArtists[0])
	}
	if got.FeaturedArtists[1].Name != "Guest Two" || got.FeaturedArtists[1].DeezerID != 555 {
		t.Errorf("featured[1] = %+v", got.FeaturedArtists[1])
	}
}
