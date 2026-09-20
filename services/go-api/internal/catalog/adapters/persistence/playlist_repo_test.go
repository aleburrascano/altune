package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestPlaylistForDB(t *testing.T, userId shared.UserId) *domain.Playlist {
	t.Helper()
	pl, err := domain.NewPlaylist(userId, "Playlist-"+uuid.New().String()[:8], time.Now())
	if err != nil {
		t.Fatalf("newTestPlaylistForDB: %v", err)
	}
	return pl
}

// seedTrackForDB stores one fresh track owned by userId and returns its id.
func seedTrackForDB(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userId shared.UserId) domain.TrackId {
	t.Helper()
	track := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track.ID, userId)
	if _, _, err := NewPgxTrackRepository(pool).Add(ctx, track); err != nil {
		t.Fatalf("seedTrackForDB: %v", err)
	}
	return track.ID
}

func cleanupPlaylist(t *testing.T, pool *pgxpool.Pool, id domain.PlaylistId, userId shared.UserId) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM playlist_tracks WHERE playlist_id = $1`, id.UUID())
		_, _ = pool.Exec(ctx, `DELETE FROM playlists WHERE id = $1 AND user_id = $2`, id.UUID(), userId.UUID())
	})
}

// CountForUser feeds the per-user playlist cap (#2200): it counts only the
// caller's rows, and stops at atMost so the query cost does not grow with an
// account that is already far past the cap.
func TestPgxPlaylistRepo_CountForUser_CountsOwnedRowsAndStopsAtTheBound(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	otherId := shared.NewUserId(uuid.New())

	for range 3 {
		seedPlaylistForDB(ctx, t, pool, userId)
	}
	seedPlaylistForDB(ctx, t, pool, otherId)

	whole, err := repo.CountForUser(ctx, userId, 10)
	if err != nil {
		t.Fatalf("CountForUser(10): %v", err)
	}
	if whole != 3 {
		t.Errorf("CountForUser(10) = %d, want 3 (another owner's playlist must not count)", whole)
	}

	bounded, err := repo.CountForUser(ctx, userId, 2)
	if err != nil {
		t.Fatalf("CountForUser(2): %v", err)
	}
	if bounded != 2 {
		t.Errorf("CountForUser(2) = %d, want 2: the count must stop at the bound", bounded)
	}
}

// seedPlaylistForDB stores one fresh playlist owned by userId.
func seedPlaylistForDB(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userId shared.UserId) domain.PlaylistId {
	t.Helper()
	pl := newTestPlaylistForDB(t, userId)
	cleanupPlaylist(t, pool, pl.ID, userId)
	if err := NewPgxPlaylistRepository(pool).Create(ctx, pl); err != nil {
		t.Fatalf("seedPlaylistForDB: %v", err)
	}
	return pl.ID
}

func TestPgxPlaylistRepo_CreateAndGetByID(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	pl := newTestPlaylistForDB(t, userId)
	cleanupPlaylist(t, pool, pl.ID, userId)

	if err := repo.Create(ctx, pl); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, _, err := repo.GetByID(ctx, pl.ID, userId)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got == nil {
		t.Fatal("GetByID() returned nil, want playlist")
	}

	if got.ID.UUID() != pl.ID.UUID() {
		t.Errorf("ID = %v, want %v", got.ID.UUID(), pl.ID.UUID())
	}
	if got.UserId.UUID() != userId.UUID() {
		t.Errorf("UserId = %v, want %v", got.UserId.UUID(), userId.UUID())
	}
	if got.Name != pl.Name {
		t.Errorf("Name = %q, want %q", got.Name, pl.Name)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt is zero")
	}
}

func TestPgxPlaylistRepo_ListForUser(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	for i := 0; i < 3; i++ {
		pl := newTestPlaylistForDB(t, userId)
		pl.CreatedAt = time.Now().UTC().Add(time.Duration(i) * time.Second)
		pl.UpdatedAt = pl.CreatedAt
		cleanupPlaylist(t, pool, pl.ID, userId)
		if err := repo.Create(ctx, pl); err != nil {
			t.Fatalf("Create playlist %d: %v", i, err)
		}
	}

	got, err := repo.ListForUser(ctx, userId, 10, 0)
	if err != nil {
		t.Fatalf("ListForUser() error = %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("len(playlists) = %d, want 3", len(got))
	}

	for i := 1; i < len(got); i++ {
		if got[i-1].Playlist.CreatedAt.Before(got[i].Playlist.CreatedAt) {
			t.Errorf("playlists not in descending created_at order at index %d", i)
		}
	}
}

func TestPgxPlaylistRepo_ListForUserPagesWithoutRepeatingARow(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	// One shared instant: the tiebreak, not created_at, is what keeps the two
	// pages disjoint here.
	createdAt := time.Now().UTC()
	for i := 0; i < 3; i++ {
		pl := newTestPlaylistForDB(t, userId)
		pl.CreatedAt = createdAt
		pl.UpdatedAt = createdAt
		cleanupPlaylist(t, pool, pl.ID, userId)
		if err := repo.Create(ctx, pl); err != nil {
			t.Fatalf("Create playlist %d: %v", i, err)
		}
	}

	first, err := repo.ListForUser(ctx, userId, 2, 0)
	if err != nil {
		t.Fatalf("ListForUser(limit=2, offset=0): %v", err)
	}
	second, err := repo.ListForUser(ctx, userId, 2, 2)
	if err != nil {
		t.Fatalf("ListForUser(limit=2, offset=2): %v", err)
	}

	if len(first) != 2 || len(second) != 1 {
		t.Fatalf("page sizes = %d and %d, want 2 and 1", len(first), len(second))
	}
	seen := map[string]bool{}
	for _, ps := range append(first, second...) {
		if seen[ps.Playlist.ID.String()] {
			t.Errorf("playlist %s served by both pages", ps.Playlist.ID)
		}
		seen[ps.Playlist.ID.String()] = true
	}
	if len(seen) != 3 {
		t.Errorf("the two pages covered %d playlists, want all 3", len(seen))
	}
}

func TestPgxPlaylistRepo_Delete(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	pl := newTestPlaylistForDB(t, userId)
	cleanupPlaylist(t, pool, pl.ID, userId)

	if err := repo.Create(ctx, pl); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	deleted, err := repo.Delete(ctx, pl.ID, userId)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !deleted {
		t.Error("Delete() deleted = false, want true")
	}

	got, _, err := repo.GetByID(ctx, pl.ID, userId)
	if err != nil {
		t.Fatalf("GetByID after delete: %v", err)
	}
	if got != nil {
		t.Errorf("GetByID after delete returned non-nil: %v", got.ID)
	}

	deleted2, err := repo.Delete(ctx, pl.ID, userId)
	if err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
	if deleted2 {
		t.Error("second Delete() deleted = true, want false (already gone)")
	}
}

func TestPgxPlaylistRepo_AddAndRemoveTrack(t *testing.T) {
	pool := testPool(t)
	playlistRepo := NewPgxPlaylistRepository(pool)
	trackRepo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	pl := newTestPlaylistForDB(t, userId)
	cleanupPlaylist(t, pool, pl.ID, userId)
	if err := playlistRepo.Create(ctx, pl); err != nil {
		t.Fatalf("Create playlist: %v", err)
	}

	track := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track.ID, userId)
	if _, _, err := trackRepo.Add(ctx, track); err != nil {
		t.Fatalf("Add track: %v", err)
	}

	if err := playlistRepo.AddTrack(ctx, userId, pl.ID, track.ID); err != nil {
		t.Fatalf("AddTrack() error = %v", err)
	}

	gotPl, gotTracks, err := playlistRepo.GetWithTracks(ctx, pl.ID, userId)
	if err != nil {
		t.Fatalf("GetWithTracks() error = %v", err)
	}
	if gotPl == nil {
		t.Fatal("GetWithTracks() returned nil playlist")
	}
	if len(gotTracks) != 1 {
		t.Fatalf("len(tracks) = %d, want 1", len(gotTracks))
	}
	if gotTracks[0].ID.UUID() != track.ID.UUID() {
		t.Errorf("track ID = %v, want %v", gotTracks[0].ID.UUID(), track.ID.UUID())
	}
	if len(gotPl.Tracks) != 1 {
		t.Fatalf("len(playlist.Tracks) = %d, want 1", len(gotPl.Tracks))
	}
	if gotPl.Tracks[0].Position != 0 {
		t.Errorf("track position = %d, want 0", gotPl.Tracks[0].Position)
	}

	removed, err := playlistRepo.RemoveTrack(ctx, userId, pl.ID, track.ID)
	if err != nil {
		t.Fatalf("RemoveTrack() error = %v", err)
	}
	if !removed {
		t.Fatal("RemoveTrack() removed = false, want true for a member")
	}

	gotPl2, gotTracks2, err := playlistRepo.GetWithTracks(ctx, pl.ID, userId)
	if err != nil {
		t.Fatalf("GetWithTracks after remove: %v", err)
	}
	if gotPl2 == nil {
		t.Fatal("GetWithTracks after remove returned nil playlist")
	}
	if len(gotTracks2) != 0 {
		t.Errorf("len(tracks) after remove = %d, want 0", len(gotTracks2))
	}
}

func TestPgxPlaylistRepo_ReorderTracks(t *testing.T) {
	pool := testPool(t)
	playlistRepo := NewPgxPlaylistRepository(pool)
	trackRepo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	pl := newTestPlaylistForDB(t, userId)
	cleanupPlaylist(t, pool, pl.ID, userId)
	if err := playlistRepo.Create(ctx, pl); err != nil {
		t.Fatalf("Create playlist: %v", err)
	}

	trackIDs := make([]domain.TrackId, 3)
	for i := 0; i < 3; i++ {
		track := newTestTrackForDB(t, userId)
		cleanupTrack(t, pool, track.ID, userId)
		if _, _, err := trackRepo.Add(ctx, track); err != nil {
			t.Fatalf("Add track %d: %v", i, err)
		}
		if err := playlistRepo.AddTrack(ctx, userId, pl.ID, track.ID); err != nil {
			t.Fatalf("AddTrack %d: %v", i, err)
		}
		trackIDs[i] = track.ID
	}

	reordered := []domain.PlaylistTrack{
		{TrackId: trackIDs[2], Position: 0},
		{TrackId: trackIDs[1], Position: 1},
		{TrackId: trackIDs[0], Position: 2},
	}
	if err := playlistRepo.ReorderTracks(ctx, userId, pl.ID, reordered); err != nil {
		t.Fatalf("ReorderTracks() error = %v", err)
	}

	gotPl, _, err := playlistRepo.GetWithTracks(ctx, pl.ID, userId)
	if err != nil {
		t.Fatalf("GetWithTracks after reorder: %v", err)
	}
	if len(gotPl.Tracks) != 3 {
		t.Fatalf("len(tracks) = %d, want 3", len(gotPl.Tracks))
	}

	if gotPl.Tracks[0].TrackId.UUID() != trackIDs[2].UUID() {
		t.Errorf("position 0: track = %v, want %v", gotPl.Tracks[0].TrackId.UUID(), trackIDs[2].UUID())
	}
	if gotPl.Tracks[1].TrackId.UUID() != trackIDs[1].UUID() {
		t.Errorf("position 1: track = %v, want %v", gotPl.Tracks[1].TrackId.UUID(), trackIDs[1].UUID())
	}
	if gotPl.Tracks[2].TrackId.UUID() != trackIDs[0].UUID() {
		t.Errorf("position 2: track = %v, want %v", gotPl.Tracks[2].TrackId.UUID(), trackIDs[0].UUID())
	}
}

func TestPgxPlaylistRepo_GetByID_NotFound(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	got, _, err := repo.GetByID(ctx, domain.PlaylistIdFromUUID(uuid.New()), userId)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got != nil {
		t.Errorf("GetByID() for missing playlist returned non-nil: %v", got.ID)
	}
}

// withPlaylistTrackCap lowers the playlist bound for one test, so crossing it
// costs four rows rather than two thousand.
func withPlaylistTrackCap(t *testing.T, limit int) {
	t.Helper()
	prev := maxPlaylistTracks
	maxPlaylistTracks = limit
	t.Cleanup(func() { maxPlaylistTracks = prev })
}

func TestPgxPlaylistRepo_GetWithTracks_BoundedByLimit(t *testing.T) {
	pool := testPool(t)
	playlistRepo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	// Seeded before the bound is lowered: the same number now caps adds, so a
	// playlist can only be over it the way the real ones are — it grew there
	// while the bound was higher.
	const inserted = 5
	pl, _ := seedPlaylistWithTracks(ctx, t, pool, userId, inserted)
	withPlaylistTrackCap(t, 3)

	_, gotTracks, err := playlistRepo.GetWithTracks(ctx, pl.ID, userId)
	if err != nil {
		t.Fatalf("GetWithTracks() error = %v", err)
	}
	if len(gotTracks) != maxPlaylistTracks {
		t.Fatalf("len(tracks) = %d, want %d (bounded by limit, %d inserted)",
			len(gotTracks), maxPlaylistTracks, inserted)
	}
}

// TestPgxPlaylistRepo_AddTrack_RefusedPastTheCap reproduces #2196: no total
// size cap existed, so a playlist could grow past the bound every read stops
// at and silently lose its tail. The add that would cross the cap is refused
// under the playlist lock, leaving the playlist exactly as it was.
func TestPgxPlaylistRepo_AddTrack_RefusedPastTheCap(t *testing.T) {
	pool := testPool(t)
	playlistRepo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	withPlaylistTrackCap(t, 3)
	pl, ids := seedPlaylistWithTracks(ctx, t, pool, userId, 3)
	over := seedTrackForDB(ctx, t, pool, userId)

	err := playlistRepo.AddTrack(ctx, userId, pl.ID, over)

	if !errors.Is(err, domain.ErrPlaylistFull) {
		t.Fatalf("AddTrack past the cap: err = %v, want domain.ErrPlaylistFull", err)
	}
	assertContiguousOrder(ctx, t, pool, pl.ID, ids)
}

// TestPgxPlaylistRepo_AddTracks_RefusesTheWholeBatchPastTheCap holds the batch
// to all-or-nothing: a batch whose new members would cross the cap inserts
// none of them, rather than filling the remaining slots and dropping the rest.
func TestPgxPlaylistRepo_AddTracks_RefusesTheWholeBatchPastTheCap(t *testing.T) {
	pool := testPool(t)
	playlistRepo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	withPlaylistTrackCap(t, 3)
	pl, ids := seedPlaylistWithTracks(ctx, t, pool, userId, 2)
	batch := []domain.TrackId{seedTrackForDB(ctx, t, pool, userId), seedTrackForDB(ctx, t, pool, userId)}

	added, err := playlistRepo.AddTracks(ctx, userId, pl.ID, batch)

	if !errors.Is(err, domain.ErrPlaylistFull) {
		t.Fatalf("AddTracks past the cap: err = %v, want domain.ErrPlaylistFull", err)
	}
	if len(added) != 0 {
		t.Errorf("added = %v, want none", added)
	}
	assertContiguousOrder(ctx, t, pool, pl.ID, ids)
}

// TestPgxPlaylistRepo_OverCapPlaylist_StaysReadableAndStopsGrowing covers the
// playlists that passed the cap before it existed: the cap may not turn them
// into an error, and the one way out of the state — removing tracks — has to
// keep working.
func TestPgxPlaylistRepo_OverCapPlaylist_StaysReadableAndStopsGrowing(t *testing.T) {
	pool := testPool(t)
	playlistRepo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	pl, ids := seedPlaylistWithTracks(ctx, t, pool, userId, 5)
	withPlaylistTrackCap(t, 3)

	t.Run("still reads, bounded", func(t *testing.T) {
		order, found, err := playlistRepo.GetTrackOrder(ctx, pl.ID, userId)
		if err != nil || !found {
			t.Fatalf("GetTrackOrder = found %v, err %v; want true, nil", found, err)
		}
		if !reflect.DeepEqual(order, ids[:3]) {
			t.Fatalf("order = %v, want the first 3 of %v", order, ids)
		}
	})

	t.Run("refuses a further add", func(t *testing.T) {
		err := playlistRepo.AddTrack(ctx, userId, pl.ID, seedTrackForDB(ctx, t, pool, userId))
		if !errors.Is(err, domain.ErrPlaylistFull) {
			t.Fatalf("AddTrack on an over-cap playlist: err = %v, want domain.ErrPlaylistFull", err)
		}
	})

	// A retried add of tracks the playlist already holds inserts nothing, so it
	// takes the playlist nowhere: answering it "full" would fail a request that
	// asks for no room at all.
	t.Run("re-adding tracks it already holds is not refused", func(t *testing.T) {
		if err := playlistRepo.AddTrack(ctx, userId, pl.ID, ids[1]); !errors.Is(err, domain.ErrTrackAlreadyInPlaylist) {
			t.Errorf("AddTrack(member): err = %v, want domain.ErrTrackAlreadyInPlaylist", err)
		}
		added, err := playlistRepo.AddTracks(ctx, userId, pl.ID, ids[:2])
		if err != nil {
			t.Fatalf("AddTracks(members): %v", err)
		}
		if len(added) != 0 {
			t.Errorf("added = %v, want none", added)
		}
	})

	t.Run("still removes", func(t *testing.T) {
		removed, err := playlistRepo.RemoveTrack(ctx, userId, pl.ID, ids[0])
		if err != nil || !removed {
			t.Fatalf("RemoveTrack = %v, %v; want true, nil", removed, err)
		}
	})
}

// TestPgxPlaylistRepo_MembershipWrites_RefuseForeignOwner proves the data layer
// itself owner-scopes every membership write (issue #1044): called directly with
// another tenant's playlist id — bypassing the service's loadPlaylist check —
// each write returns ports.ErrPlaylistNotOwned and leaves the victim's playlist
// exactly as it was.
func TestPgxPlaylistRepo_MembershipWrites_RefuseForeignOwner(t *testing.T) {
	pool := testPool(t)
	playlistRepo := NewPgxPlaylistRepository(pool)
	trackRepo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	victim := shared.NewUserId(uuid.New())
	attacker := shared.NewUserId(uuid.New())

	pl := newTestPlaylistForDB(t, victim)
	cleanupPlaylist(t, pool, pl.ID, victim)
	if err := playlistRepo.Create(ctx, pl); err != nil {
		t.Fatalf("Create playlist: %v", err)
	}
	const seeded = 3
	tracks := make([]*domain.Track, 0, seeded)
	for i := 0; i < seeded; i++ {
		tr := newTestTrackForDB(t, victim)
		cleanupTrack(t, pool, tr.ID, victim)
		if _, _, err := trackRepo.Add(ctx, tr); err != nil {
			t.Fatalf("Add track %d: %v", i, err)
		}
		tracks = append(tracks, tr)
	}
	for _, tr := range tracks[:2] {
		if err := playlistRepo.AddTrack(ctx, victim, pl.ID, tr.ID); err != nil {
			t.Fatalf("seed AddTrack: %v", err)
		}
	}
	outsider := tracks[2]

	snapshot := func(t *testing.T) []domain.PlaylistTrack {
		t.Helper()
		got, _, err := playlistRepo.GetWithTracks(ctx, pl.ID, victim)
		if err != nil || got == nil {
			t.Fatalf("GetWithTracks: playlist=%v err=%v", got, err)
		}
		return got.Tracks
	}
	before := snapshot(t)

	cases := []struct {
		name string
		call func() error
	}{
		{"AddTrack", func() error {
			return playlistRepo.AddTrack(ctx, attacker, pl.ID, outsider.ID)
		}},
		{"AddTracks", func() error {
			_, err := playlistRepo.AddTracks(ctx, attacker, pl.ID, []domain.TrackId{outsider.ID})
			return err
		}},
		{"RemoveTrack", func() error {
			_, err := playlistRepo.RemoveTrack(ctx, attacker, pl.ID, tracks[0].ID)
			return err
		}},
		{"RemoveTracks", func() error {
			_, err := playlistRepo.RemoveTracks(ctx, attacker, pl.ID, []domain.TrackId{tracks[0].ID, tracks[1].ID})
			return err
		}},
		{"ReorderTracks", func() error {
			return playlistRepo.ReorderTracks(ctx, attacker, pl.ID, []domain.PlaylistTrack{
				{TrackId: tracks[1].ID, Position: 0},
				{TrackId: tracks[0].ID, Position: 1},
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, ports.ErrPlaylistNotOwned) {
				t.Fatalf("%s as non-owner: err = %v, want ports.ErrPlaylistNotOwned", tc.name, err)
			}
			if after := snapshot(t); !reflect.DeepEqual(after, before) {
				t.Fatalf("%s as non-owner mutated the playlist: before %v, after %v", tc.name, before, after)
			}
		})
	}

	t.Run("missing playlist", func(t *testing.T) {
		err := playlistRepo.AddTrack(ctx, victim, domain.NewPlaylistId(), outsider.ID)
		if !errors.Is(err, ports.ErrPlaylistNotOwned) {
			t.Fatalf("AddTrack to missing playlist: err = %v, want ports.ErrPlaylistNotOwned", err)
		}
	})
}
