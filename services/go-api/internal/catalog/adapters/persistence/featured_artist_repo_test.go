package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestBuildFeaturingQuery_CapsResultSet is the regression guard for #423: the
// featuring query must carry a LIMIT bound so a match against tens of thousands
// of rows returns a bounded result instead of the full set. Runs without a DB,
// so it executes in CI (the integration tests below skip when DATABASE_URL is
// unset). On the pre-fix code the SQL had no LIMIT clause and this fails.
func TestBuildFeaturingQuery_CapsResultSet(t *testing.T) {
	sql, args := buildFeaturingQuery(uuid.New(), []string{"identity-key"})

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
	if featuringResultCap != domain.MaxLibraryPageSize {
		t.Errorf("featuringResultCap = %d, want domain.MaxLibraryPageSize %d", featuringResultCap, domain.MaxLibraryPageSize)
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

// addTrackForFeaturing inserts a fresh track for userId with no featured
// artists and registers its cleanup.
func addTrackForFeaturing(t *testing.T, pool *pgxpool.Pool, userId shared.UserId) *domain.Track {
	t.Helper()
	track := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track.ID, userId)
	if _, _, err := NewPgxTrackRepository(pool).Add(context.Background(), track); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	return track
}

func featuredRowCount(t *testing.T, pool *pgxpool.Pool, userId shared.UserId) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM featured_artists WHERE user_id = $1`, userId.UUID()).Scan(&n); err != nil {
		t.Fatalf("count featured_artists: %v", err)
	}
	return n
}

func cleanupFeaturedArtists(t *testing.T, pool *pgxpool.Pool, userId shared.UserId) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM featured_artists WHERE user_id = $1`, userId.UUID())
	})
}

// TestPgxFeaturedArtistRepo_UnicodeEquivalentNamesShareRow is the regression
// guard for #1065: NFC and NFD spellings of one name must upsert to a single
// featured_artists row. Before NFKC folding they created two rows.
func TestPgxFeaturedArtistRepo_UnicodeEquivalentNamesShareRow(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxFeaturedArtistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	cleanupFeaturedArtists(t, pool, userId)
	first := addTrackForFeaturing(t, pool, userId)
	second := addTrackForFeaturing(t, pool, userId)

	nfc := []domain.FeaturedArtist{{Name: "Beyoncé", Role: domain.RoleFeatured}}
	nfd := []domain.FeaturedArtist{{Name: "Beyoncé", Role: domain.RoleFeatured}}
	if err := repo.ReplaceFeaturedArtists(ctx, first.ID, userId, nfc); err != nil {
		t.Fatalf("ReplaceFeaturedArtists(nfc) error = %v", err)
	}
	if err := repo.ReplaceFeaturedArtists(ctx, second.ID, userId, nfd); err != nil {
		t.Fatalf("ReplaceFeaturedArtists(nfd) error = %v", err)
	}

	if n := featuredRowCount(t, pool, userId); n != 1 {
		t.Errorf("featured_artists rows = %d, want 1", n)
	}
	got, err := repo.ListTracksFeaturing(ctx, userId, nfc[0])
	if err != nil {
		t.Fatalf("ListTracksFeaturing() error = %v", err)
	}
	if len(got) != 2 {
		t.Errorf("ListTracksFeaturing() = %d tracks, want 2", len(got))
	}
}

// TestPgxFeaturedArtistRepo_LegacyKeyRowStillMatches proves the deploy needs no
// backfill: a row persisted with the pre-NFKC norm_name is reused by the upsert
// and still found by ListTracksFeaturing for the same spelling.
func TestPgxFeaturedArtistRepo_LegacyKeyRowStillMatches(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxFeaturedArtistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	cleanupFeaturedArtists(t, pool, userId)
	legacyTrack := addTrackForFeaturing(t, pool, userId)
	newTrack := addTrackForFeaturing(t, pool, userId)

	name := "ＲＯＳＡＬＩＡ"
	fa := domain.FeaturedArtist{Name: name, Role: domain.RoleFeatured}
	legacyNorm := strings.TrimPrefix(fa.LegacyIdentityKey(), "name:")
	if legacyNorm == fa.NormalizedName() {
		t.Fatal("test name must fold differently under NFKC")
	}
	var legacyID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO featured_artists (user_id, name, norm_name) VALUES ($1,$2,$3) RETURNING id`,
		userId.UUID(), name, legacyNorm).Scan(&legacyID); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO track_featured_artists (track_id, featured_artist_id, position) VALUES ($1,$2,0)`,
		legacyTrack.ID.UUID(), legacyID); err != nil {
		t.Fatalf("link legacy row: %v", err)
	}

	if err := repo.ReplaceFeaturedArtists(ctx, newTrack.ID, userId, []domain.FeaturedArtist{fa}); err != nil {
		t.Fatalf("ReplaceFeaturedArtists() error = %v", err)
	}

	if n := featuredRowCount(t, pool, userId); n != 1 {
		t.Errorf("featured_artists rows = %d, want 1 (legacy row reused)", n)
	}
	got, err := repo.ListTracksFeaturing(ctx, userId, fa)
	if err != nil {
		t.Fatalf("ListTracksFeaturing() error = %v", err)
	}
	if len(got) != 2 {
		t.Errorf("ListTracksFeaturing() = %d tracks, want 2", len(got))
	}
}
