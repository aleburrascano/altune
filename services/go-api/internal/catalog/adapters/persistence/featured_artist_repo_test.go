package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestBuildFeaturingQuery_CapsResultSet is the regression guard for #423: the
// featuring query must carry a LIMIT bound so a match against tens of thousands
// of rows returns a bounded result instead of the full set. Runs without a DB,
// so it executes in CI (the integration tests below skip when DATABASE_URL is
// unset). On the pre-fix code the SQL had no LIMIT clause and this fails.
func TestBuildFeaturingQuery_CapsResultSet(t *testing.T) {
	sql, args := buildFeaturingQuery(uuid.New(), "identity-key")

	if !strings.Contains(sql, "LIMIT $3") {
		t.Fatalf("featuring query has no LIMIT clause, result set is unbounded:\n%s", sql)
	}
	if len(args) != 3 {
		t.Fatalf("args = %d, want 3 (user, identity, limit)", len(args))
	}
	limit, ok := args[2].(int)
	if !ok {
		t.Fatalf("limit arg type = %T, want int", args[2])
	}
	if limit != featuringResultCap {
		t.Errorf("limit arg = %d, want featuringResultCap %d", limit, featuringResultCap)
	}
	if featuringResultCap != 2000 {
		t.Errorf("featuringResultCap = %d, want 2000 to match the module's clampLibraryLimit cap", featuringResultCap)
	}
}

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

	got, err := repo.GetByDedupKey(ctx, userId, track.DedupKey)
	if err != nil {
		t.Fatalf("GetByDedupKey() error = %v", err)
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
