package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPgxPlaylistRepo_ConcurrentAddTrack_NoDuplicatePositions reproduces the
// concurrent-add race from issue #420: many requests read the same playlist
// snapshot, all derive the same "next position", and all write it. The
// repository now derives the slot itself under the playlist lock (callers no
// longer pass one, issue #1061); it must assign a unique, contiguous position
// per row. Against a plain INSERT of a caller-computed position every add
// lands on the same slot and this fails.
func TestPgxPlaylistRepo_ConcurrentAddTrack_NoDuplicatePositions(t *testing.T) {
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

	// Seed track A at position 0 so the "next position" a concurrent caller
	// derives from the snapshot [A] is 1 for every one of them.
	seed := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, seed.ID, userId)
	if _, _, err := trackRepo.Add(ctx, seed); err != nil {
		t.Fatalf("Add seed track: %v", err)
	}
	if err := playlistRepo.AddTrack(ctx, userId, pl.ID, seed.ID); err != nil {
		t.Fatalf("AddTrack(seed): %v", err)
	}

	const concurrentAdds = 8
	trackIDs := make([]domain.TrackId, concurrentAdds)
	for i := range trackIDs {
		tr := newTestTrackForDB(t, userId)
		cleanupTrack(t, pool, tr.ID, userId)
		if _, _, err := trackRepo.Add(ctx, tr); err != nil {
			t.Fatalf("Add track %d: %v", i, err)
		}
		trackIDs[i] = tr.ID
	}

	start := make(chan struct{})
	errs := make([]error, concurrentAdds)
	var wg sync.WaitGroup
	for i, id := range trackIDs {
		wg.Add(1)
		go func(i int, id domain.TrackId) {
			defer wg.Done()
			<-start
			errs[i] = playlistRepo.AddTrack(ctx, userId, pl.ID, id)
		}(i, id)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent AddTrack %d: %v", i, err)
		}
	}

	positions := fetchPositions(ctx, t, pool, pl.ID)
	want := concurrentAdds + 1 // seed + the concurrent adds
	if len(positions) != want {
		t.Fatalf("row count = %d, want %d", len(positions), want)
	}
	seen := make(map[int]bool, len(positions))
	for _, p := range positions {
		if seen[p] {
			t.Fatalf("duplicate position %d detected: %v (concurrent adds corrupted ordering)", p, positions)
		}
		seen[p] = true
	}
	for i := 0; i < want; i++ {
		if !seen[i] {
			t.Fatalf("positions are not contiguous, missing %d: %v", i, positions)
		}
	}
}

// seedPlaylistWithTracks creates a playlist owned by userId holding n freshly
// added tracks at positions 0..n-1, and returns the playlist and track ids in
// position order.
func seedPlaylistWithTracks(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userId shared.UserId, n int) (*domain.Playlist, []domain.TrackId) {
	t.Helper()
	playlistRepo := NewPgxPlaylistRepository(pool)
	trackRepo := NewPgxTrackRepository(pool)

	pl := newTestPlaylistForDB(t, userId)
	cleanupPlaylist(t, pool, pl.ID, userId)
	if err := playlistRepo.Create(ctx, pl); err != nil {
		t.Fatalf("Create playlist: %v", err)
	}
	ids := make([]domain.TrackId, n)
	for i := range ids {
		tr := newTestTrackForDB(t, userId)
		cleanupTrack(t, pool, tr.ID, userId)
		if _, _, err := trackRepo.Add(ctx, tr); err != nil {
			t.Fatalf("Add track %d: %v", i, err)
		}
		if err := playlistRepo.AddTrack(ctx, userId, pl.ID, tr.ID); err != nil {
			t.Fatalf("AddTrack %d: %v", i, err)
		}
		ids[i] = tr.ID
	}
	return pl, ids
}

// fetchOrder returns the playlist's track ids in position order.
func fetchOrder(ctx context.Context, t *testing.T, pool *pgxpool.Pool, playlistId domain.PlaylistId) []uuid.UUID {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT track_id FROM playlist_tracks WHERE playlist_id = $1 ORDER BY position`, playlistId.UUID())
	if err != nil {
		t.Fatalf("query order: %v", err)
	}
	defer rows.Close()
	var order []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan track_id: %v", err)
		}
		order = append(order, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}
	return order
}

func sameOrder(a []uuid.UUID, b []domain.TrackId) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i].UUID() {
			return false
		}
	}
	return true
}

// TestPgxPlaylistRepo_ReorderTracks_WaitsForPlaylistLock proves ReorderTracks
// takes the same playlist row lock as the other membership writes (issue
// #1056): while another transaction holds that lock, a reorder must block, and
// it must complete once the holder commits. A reorder that skips the lock
// finishes immediately and this fails.
func TestPgxPlaylistRepo_ReorderTracks_WaitsForPlaylistLock(t *testing.T) {
	pool := testPool(t)
	playlistRepo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	pl, ids := seedPlaylistWithTracks(ctx, t, pool, userId, 3)

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder tx: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, `SELECT 1 FROM playlists WHERE id = $1 FOR UPDATE`, pl.ID.UUID()); err != nil {
		t.Fatalf("take playlist lock: %v", err)
	}

	reversed := []domain.PlaylistTrack{
		{TrackId: ids[2], Position: 0},
		{TrackId: ids[1], Position: 1},
		{TrackId: ids[0], Position: 2},
	}
	done := make(chan error, 1)
	go func() { done <- playlistRepo.ReorderTracks(ctx, userId, pl.ID, reversed) }()

	select {
	case err := <-done:
		t.Fatalf("ReorderTracks finished (err=%v) while the playlist lock was held; it must serialize behind it", err)
	case <-time.After(500 * time.Millisecond):
	}

	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("commit holder tx: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ReorderTracks after lock release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReorderTracks did not finish after the playlist lock was released")
	}

	want := []domain.TrackId{ids[2], ids[1], ids[0]}
	if got := fetchOrder(ctx, t, pool, pl.ID); !sameOrder(got, want) {
		t.Fatalf("order after reorder = %v, want %v", got, want)
	}
}

// TestPgxPlaylistRepo_ConcurrentReorderTracks_Serialize races two reorders of
// the same playlist that write their per-track updates in opposite row order
// (drag-and-drop retry, or one user on two devices). Without the playlist lock
// the two transactions lock the rows in opposite order and Postgres aborts one
// with a deadlock. Under the lock both must succeed, and the final order must be
// exactly one of the two requested orders, never a blend.
func TestPgxPlaylistRepo_ConcurrentReorderTracks_Serialize(t *testing.T) {
	pool := testPool(t)
	playlistRepo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	const n = 20
	pl, ids := seedPlaylistWithTracks(ctx, t, pool, userId, n)

	// Forward keeps the seeded order, updating rows first-to-last. Reverse
	// flips it, updating rows last-to-first, so the two lock rows in opposite
	// order.
	forward := make([]domain.PlaylistTrack, n)
	reverse := make([]domain.PlaylistTrack, n)
	forwardOrder := make([]domain.TrackId, n)
	reverseOrder := make([]domain.TrackId, n)
	for i := 0; i < n; i++ {
		forward[i] = domain.PlaylistTrack{TrackId: ids[i], Position: i}
		reverse[i] = domain.PlaylistTrack{TrackId: ids[n-1-i], Position: i}
		forwardOrder[i] = ids[i]
		reverseOrder[i] = ids[n-1-i]
	}

	const rounds = 25
	for round := 0; round < rounds; round++ {
		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for k, tracks := range [][]domain.PlaylistTrack{forward, reverse} {
			wg.Add(1)
			go func(k int, tracks []domain.PlaylistTrack) {
				defer wg.Done()
				<-start
				errs[k] = playlistRepo.ReorderTracks(ctx, userId, pl.ID, tracks)
			}(k, tracks)
		}
		close(start)
		wg.Wait()

		for k, err := range errs {
			if err != nil {
				t.Fatalf("round %d: concurrent ReorderTracks %d: %v", round, k, err)
			}
		}
		got := fetchOrder(ctx, t, pool, pl.ID)
		if !sameOrder(got, forwardOrder) && !sameOrder(got, reverseOrder) {
			t.Fatalf("round %d: final order is a blend of both reorders: %v", round, got)
		}
	}
}

func fetchPositions(ctx context.Context, t *testing.T, pool *pgxpool.Pool, playlistId domain.PlaylistId) []int {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT position FROM playlist_tracks WHERE playlist_id = $1`, playlistId.UUID())
	if err != nil {
		t.Fatalf("query positions: %v", err)
	}
	defer rows.Close()
	var positions []int
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan position: %v", err)
		}
		positions = append(positions, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}
	sort.Ints(positions)
	return positions
}
