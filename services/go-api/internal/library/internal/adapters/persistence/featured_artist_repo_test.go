package persistence_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/dbtest"
	sharedids "altune/go-api/internal/shared/ids"
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresTrackRepo_FeaturedArtistsRoundTrip(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := library.NewPostgresTrackRepository(pool, librarytest.DiscardDBCallMetrics())
	ctx := context.Background()
	userId := sharedids.NewUserId(uuid.New())

	track := newTestTrackForDB(t, userId)
	track.FeaturedArtists = []library.FeaturedArtist{
		{Name: "Guest One", MBID: "mb-guest-1", Role: library.RoleFeatured},
		{Name: "Guest Two", DeezerID: 555, Role: library.RoleFeatured},
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

func TestPostgresFeaturedArtistRepo_ReplaceFeaturedArtists_RefusesAnotherUsersTrackAndLeavesItsArtists(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := library.NewPostgresCatalogTrackRepository(pool, librarytest.DiscardDBCallMetrics())
	ctx := context.Background()
	owner, intruder := sharedids.NewUserId(uuid.New()), sharedids.NewUserId(uuid.New())
	cleanupFeaturedArtists(t, pool, owner)
	cleanupFeaturedArtists(t, pool, intruder)
	track := newTestTrackForDB(t, owner)
	track.FeaturedArtists = []library.FeaturedArtist{{Name: "Guest", Role: library.RoleFeatured}}
	cleanupTrack(t, pool, track.ID, owner)
	if _, _, err := repo.Add(ctx, track); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	err := repo.ReplaceFeaturedArtists(ctx, track.ID, intruder, []library.FeaturedArtist{{Name: "Intruder", Role: library.RoleFeatured}})

	if err == nil {
		t.Fatal("ReplaceFeaturedArtists() on another user's track succeeded, want an error")
	}
	got, err := repo.GetByDedupKey(ctx, owner, track.DedupKey)
	if err != nil {
		t.Fatalf("GetByDedupKey() error = %v", err)
	}
	if len(got.FeaturedArtists) != 1 || got.FeaturedArtists[0].Name != "Guest" {
		t.Errorf("owner's featured artists = %+v, want only Guest", got.FeaturedArtists)
	}
	if n := featuredRowCount(t, pool, intruder); n != 0 {
		t.Errorf("intruder's featured_artists rows = %d, want 0", n)
	}
}

func addTrackForFeaturing(t *testing.T, pool *pgxpool.Pool, userId sharedids.UserId) *library.Track {
	t.Helper()
	track := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track.ID, userId)
	if _, _, err := library.NewPostgresTrackRepository(pool, librarytest.DiscardDBCallMetrics()).Add(context.Background(), track); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	return track
}

func featuredRowCount(t *testing.T, pool *pgxpool.Pool, userId sharedids.UserId) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM featured_artists WHERE user_id = $1`, userId.UUID()).Scan(&n); err != nil {
		t.Fatalf("count featured_artists: %v", err)
	}
	return n
}

func cleanupFeaturedArtists(t *testing.T, pool *pgxpool.Pool, userId sharedids.UserId) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM featured_artists WHERE user_id = $1`, userId.UUID())
	})
}

func TestPostgresFeaturedArtistRepo_UnicodeEquivalentNamesShareRow(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := library.NewPostgresCatalogTrackRepository(pool, librarytest.DiscardDBCallMetrics())
	ctx := context.Background()
	userId := sharedids.NewUserId(uuid.New())
	cleanupFeaturedArtists(t, pool, userId)
	first := addTrackForFeaturing(t, pool, userId)
	second := addTrackForFeaturing(t, pool, userId)

	nfc := []library.FeaturedArtist{{Name: "Beyoncé", Role: library.RoleFeatured}}
	nfd := []library.FeaturedArtist{{Name: "Beyoncé", Role: library.RoleFeatured}}
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

func TestPostgresFeaturedArtistRepo_LegacyKeyRowStillMatches(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := library.NewPostgresCatalogTrackRepository(pool, librarytest.DiscardDBCallMetrics())
	ctx := context.Background()
	userId := sharedids.NewUserId(uuid.New())
	cleanupFeaturedArtists(t, pool, userId)
	legacyTrack := addTrackForFeaturing(t, pool, userId)
	newTrack := addTrackForFeaturing(t, pool, userId)

	name := "ＲＯＳＡＬＩＡ"
	fa := library.FeaturedArtist{Name: name, Role: library.RoleFeatured}
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

	if err := repo.ReplaceFeaturedArtists(ctx, newTrack.ID, userId, []library.FeaturedArtist{fa}); err != nil {
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

const bulkFeaturingSQL = `INSERT INTO track_featured_artists (track_id, featured_artist_id, position)
	SELECT t.id, $2, 0 FROM unnest($1::uuid[]) AS t(id)`

func seedFeaturedOnAll(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userId sharedids.UserId, fa library.FeaturedArtist, ids []library.TrackId) {
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

func ascendingTrackIds(n int) []library.TrackId {
	ids := make([]library.TrackId, n)
	for i := range ids {
		ids[i] = librarytest.NewTrackId()
	}
	slices.SortFunc(ids, func(a, b library.TrackId) int {
		x, y := a.UUID(), b.UUID()
		return bytes.Compare(x[:], y[:])
	})
	return ids
}

func TestPostgresFeaturedArtistRepo_ListTracksFeaturing_StopsAtTheLibraryPageCap(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	userId := sharedids.NewUserId(uuid.New())
	cleanupFeaturedArtists(t, pool, userId)
	ids := ascendingTrackIds(library.MaxLibraryPageSize + 1)
	insertBulkTracks(ctx, t, pool, userId, ids)
	fa := library.FeaturedArtist{Name: "Capped Guest", MBID: "mb-capped-guest", Role: library.RoleFeatured}
	seedFeaturedOnAll(ctx, t, pool, userId, fa, ids)

	got, err := library.NewPostgresCatalogTrackRepository(pool, librarytest.DiscardDBCallMetrics()).ListTracksFeaturing(ctx, userId, fa)

	if err != nil || len(got) != library.MaxLibraryPageSize {
		t.Fatalf("ListTracksFeaturing = %d tracks, err %v; want the %d cap of %d featuring", len(got), err, library.MaxLibraryPageSize, len(ids))
	}
	for i, track := range got {
		if want := ids[len(ids)-1-i]; track.ID != want {
			t.Fatalf("track %d = %s, want %s: tracks added together come largest id first, and the cap drops the smallest, which sits first on disk", i, track.ID, want)
		}
	}
}

func addFeaturedTrackAt(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userId sharedids.UserId, id library.TrackId, addedAt time.Time, fa library.FeaturedArtist) {
	t.Helper()
	track := newTestTrackForDB(t, userId)
	track.ID = id
	track.AddedAt = addedAt
	cleanupTrack(t, pool, id, userId)
	repo := library.NewPostgresCatalogTrackRepository(pool, librarytest.DiscardDBCallMetrics())
	if _, _, err := repo.Add(ctx, track); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := repo.ReplaceFeaturedArtists(ctx, id, userId, []library.FeaturedArtist{fa}); err != nil {
		t.Fatalf("ReplaceFeaturedArtists() error = %v", err)
	}
}

func featuringIds(tracks []*library.Track) []library.TrackId {
	ids := make([]library.TrackId, len(tracks))
	for i, track := range tracks {
		ids[i] = track.ID
	}
	return ids
}

func TestPostgresFeaturedArtistRepo_ListTracksFeaturing_LeavesOutAnotherUsersTracks(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	owner, other := sharedids.NewUserId(uuid.New()), sharedids.NewUserId(uuid.New())
	cleanupFeaturedArtists(t, pool, owner)
	cleanupFeaturedArtists(t, pool, other)
	fa := library.FeaturedArtist{Name: "Shared Guest", MBID: "mb-shared-guest-" + uuid.NewString(), Role: library.RoleFeatured}
	ids := ascendingTrackIds(2)
	addedAt := time.Now().UTC().Truncate(time.Second)
	addFeaturedTrackAt(ctx, t, pool, owner, ids[0], addedAt, fa)
	addFeaturedTrackAt(ctx, t, pool, other, ids[1], addedAt, fa)

	got, err := library.NewPostgresCatalogTrackRepository(pool, librarytest.DiscardDBCallMetrics()).ListTracksFeaturing(ctx, owner, fa)

	if err != nil || !slices.Equal(featuringIds(got), ids[:1]) {
		t.Fatalf("ListTracksFeaturing(owner) = %v, err %v; want only the owner's track %v, none of the other user's", featuringIds(got), err, ids[:1])
	}
}

func TestPostgresFeaturedArtistRepo_ListTracksFeaturing_NewestAddedComesFirst(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	userId := sharedids.NewUserId(uuid.New())
	cleanupFeaturedArtists(t, pool, userId)
	fa := library.FeaturedArtist{Name: "Ordered Guest", MBID: "mb-ordered-guest-" + uuid.NewString(), Role: library.RoleFeatured}
	ids := ascendingTrackIds(2)
	addedAt := time.Now().UTC().Truncate(time.Second)
	addFeaturedTrackAt(ctx, t, pool, userId, ids[0], addedAt, fa)
	addFeaturedTrackAt(ctx, t, pool, userId, ids[1], addedAt.Add(-time.Hour), fa)

	got, err := library.NewPostgresCatalogTrackRepository(pool, librarytest.DiscardDBCallMetrics()).ListTracksFeaturing(ctx, userId, fa)

	if err != nil || !slices.Equal(featuringIds(got), ids) {
		t.Fatalf("ListTracksFeaturing = %v, err %v; want %v: the newest added first, though its id is the smaller", featuringIds(got), err, ids)
	}
}
