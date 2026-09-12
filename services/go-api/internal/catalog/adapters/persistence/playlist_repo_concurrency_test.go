package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"sort"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPgxPlaylistRepo_ConcurrentAddTrack_NoDuplicatePositions reproduces the
// concurrent-add race from issue #420: many requests read the same playlist
// snapshot, all derive the same "next position", and all write it. Each caller
// below passes the same stale position on purpose (mirroring what the service
// computes from a shared snapshot); the repository must still assign a unique,
// contiguous position per row. Against the pre-fix repository (a plain INSERT of
// the caller's position) every add lands on the same slot and this fails.
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
	if err := playlistRepo.AddTrack(ctx, pl.ID, seed.ID, 0); err != nil {
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

	// The stale snapshot every concurrent caller would observe: [A], so the
	// "next position" they all compute is 1.
	const stalePosition = 1

	start := make(chan struct{})
	errs := make([]error, concurrentAdds)
	var wg sync.WaitGroup
	for i, id := range trackIDs {
		wg.Add(1)
		go func(i int, id domain.TrackId) {
			defer wg.Done()
			<-start
			errs[i] = playlistRepo.AddTrack(ctx, pl.ID, id, stalePosition)
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
