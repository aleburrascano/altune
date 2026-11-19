package persistence_test

import (
	"altune/go-api/internal/catalog"
	shared "altune/go-api/internal/shared/kernel"
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPgxTrackRepo_FeaturedArtistsRoundTrip(t *testing.T) {
	pool := testPool(t)
	repo := catalog.NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	track := newTestTrackForDB(t, userId)
	track.FeaturedArtists = []catalog.FeaturedArtist{
		{Name: "Guest One", MBID: "mb-guest-1", Role: catalog.RoleFeatured},
		{Name: "Guest Two", DeezerID: 555, Role: catalog.RoleFeatured},
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

func addTrackForFeaturing(t *testing.T, pool *pgxpool.Pool, userId shared.UserId) *catalog.Track {
	t.Helper()
	track := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track.ID, userId)
	if _, _, err := catalog.NewPgxTrackRepository(pool).Add(context.Background(), track); err != nil {
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

func TestPgxFeaturedArtistRepo_UnicodeEquivalentNamesShareRow(t *testing.T) {
	pool := testPool(t)
	repo := catalog.NewPgxCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	cleanupFeaturedArtists(t, pool, userId)
	first := addTrackForFeaturing(t, pool, userId)
	second := addTrackForFeaturing(t, pool, userId)

	nfc := []catalog.FeaturedArtist{{Name: "Beyoncé", Role: catalog.RoleFeatured}}
	nfd := []catalog.FeaturedArtist{{Name: "Beyoncé", Role: catalog.RoleFeatured}}
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

func TestPgxFeaturedArtistRepo_LegacyKeyRowStillMatches(t *testing.T) {
	pool := testPool(t)
	repo := catalog.NewPgxCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	cleanupFeaturedArtists(t, pool, userId)
	legacyTrack := addTrackForFeaturing(t, pool, userId)
	newTrack := addTrackForFeaturing(t, pool, userId)

	name := "ＲＯＳＡＬＩＡ"
	fa := catalog.FeaturedArtist{Name: name, Role: catalog.RoleFeatured}
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

	if err := repo.ReplaceFeaturedArtists(ctx, newTrack.ID, userId, []catalog.FeaturedArtist{fa}); err != nil {
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

const featuringResultCap = 2000

const bulkFeaturingSQL = `INSERT INTO track_featured_artists (track_id, featured_artist_id, position)
	SELECT t.id, $2, 0 FROM unnest($1::uuid[]) AS t(id)`

func seedFeaturedOnAll(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userId shared.UserId, fa catalog.FeaturedArtist, ids []catalog.TrackId) {
	t.Helper()
	var faID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO featured_artists (user_id, mbid, name, norm_name) VALUES ($1, $2, $3, $4) RETURNING id`,
		userId.UUID(), fa.MBID, fa.Name, fa.NormalizedName()).Scan(&faID); err != nil {
		t.Fatalf("insert featured artist: %v", err)
	}
	if _, err := pool.Exec(ctx, bulkFeaturingSQL, trackUUIDs(ids), faID); err != nil {
		t.Fatalf("link %d tracks: %v", len(ids), err)
	}
}

func ascendingTrackIds(n int) []catalog.TrackId {
	ids := make([]catalog.TrackId, n)
	for i := range ids {
		ids[i] = catalog.NewTrackId()
	}
	slices.SortFunc(ids, func(a, b catalog.TrackId) int {
		x, y := a.UUID(), b.UUID()
		return bytes.Compare(x[:], y[:])
	})
	return ids
}

func TestPgxFeaturedArtistRepo_ListTracksFeaturing_StopsAtTheLibraryPageCap(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	userId := shared.NewUserId(uuid.New())
	cleanupFeaturedArtists(t, pool, userId)
	ids := ascendingTrackIds(featuringResultCap + 1)
	insertBulkTracks(ctx, t, pool, userId, ids)
	fa := catalog.FeaturedArtist{Name: "Capped Guest", MBID: "mb-capped-guest", Role: catalog.RoleFeatured}
	seedFeaturedOnAll(ctx, t, pool, userId, fa, ids)

	got, err := catalog.NewPgxCatalogTrackRepository(pool).ListTracksFeaturing(ctx, userId, fa)

	if err != nil || len(got) != featuringResultCap {
		t.Fatalf("ListTracksFeaturing = %d tracks, err %v; want the %d cap of %d featuring", len(got), err, featuringResultCap, len(ids))
	}
	for i, track := range got {
		if want := ids[len(ids)-1-i]; track.ID != want {
			t.Fatalf("track %d = %s, want %s: tracks added together come largest id first, and the cap drops the smallest, which sits first on disk", i, track.ID, want)
		}
	}
}
