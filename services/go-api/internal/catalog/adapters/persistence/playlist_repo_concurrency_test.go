package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
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

// TestPgxPlaylistRepo_ConcurrentAddsRaceTheLastSlot_OneWins attacks the size
// cap of #2196 the way a double-spend attacks a balance: many callers reach
// for the one free slot at once. The count is taken inside the same
// owner-scoped lock as the insert and the refusal aborts the transaction, so
// exactly one add commits; against a cap checked on an unlocked read they all
// would.
func TestPgxPlaylistRepo_ConcurrentAddsRaceTheLastSlot_OneWins(t *testing.T) {
	pool := testPool(t)
	playlistRepo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	const capacity = 3
	withPlaylistTrackCap(t, capacity)
	pl, ids := seedPlaylistWithTracks(ctx, t, pool, userId, capacity-1)

	const callers = 8
	contenders := make([]domain.TrackId, callers)
	for i := range contenders {
		contenders[i] = seedTrackForDB(ctx, t, pool, userId)
	}
	errs := make([]error, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, id := range contenders {
		wg.Add(1)
		go func(i int, id domain.TrackId) {
			defer wg.Done()
			<-start
			errs[i] = playlistRepo.AddTrack(ctx, userId, pl.ID, id)
		}(i, id)
	}
	close(start)
	wg.Wait()

	wins := 0
	for i, err := range errs {
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, domain.ErrPlaylistFull):
			t.Fatalf("caller %d: err = %v, want nil or domain.ErrPlaylistFull", i, err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d callers took the last slot, want exactly 1", wins)
	}
	if got := fetchOrder(ctx, t, pool, pl.ID); len(got) != capacity {
		t.Fatalf("playlist holds %d tracks, want %d (seeded %v)", len(got), capacity, ids)
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

// positionPlan is the reorder plan a caller builds from an order it has just
// read: every track at its index.
func positionPlan(ids []domain.TrackId) []domain.PlaylistTrack {
	plan := make([]domain.PlaylistTrack, len(ids))
	for i, id := range ids {
		plan[i] = domain.PlaylistTrack{TrackId: id, Position: i}
	}
	return plan
}

// assertPositionsAreContiguous fails unless the playlist holds want rows at
// positions 0..want-1, so a refused reorder is shown to have left neither two
// tracks on one slot nor a hole where a removed one was.
func assertPositionsAreContiguous(ctx context.Context, t *testing.T, pool *pgxpool.Pool, playlistId domain.PlaylistId, want int) {
	t.Helper()
	positions := fetchPositions(ctx, t, pool, playlistId)
	if len(positions) != want {
		t.Fatalf("row count = %d, want %d", len(positions), want)
	}
	for i, p := range positions {
		if p != i {
			t.Fatalf("positions = %v, want the contiguous run 0..%d", positions, want-1)
		}
	}
}

// TestPgxPlaylistRepo_ReorderTracks_RefusesAPlanStaleFromARemoveAndAdd
// reproduces issue #2197's reorder race: the caller reads the order without a
// lock, a remove and an add commit, and the plan it then writes moves a
// survivor onto the slot the new track took. The deferred unique constraint
// catches that at COMMIT, which reaches the client as a 500; the plan must be
// refused as stale instead.
func TestPgxPlaylistRepo_ReorderTracks_RefusesAPlanStaleFromARemoveAndAdd(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	pl, ids := seedPlaylistWithTracks(ctx, t, pool, userId, 3)
	plan := positionPlan(ids)

	if _, err := repo.RemoveTrack(ctx, userId, pl.ID, ids[0]); err != nil {
		t.Fatalf("RemoveTrack: %v", err)
	}
	addTrackToPlaylist(ctx, t, pool, userId, pl.ID)

	err := repo.ReorderTracks(ctx, userId, pl.ID, plan)

	if !errors.Is(err, ports.ErrPlaylistChangedDuringReorder) {
		t.Fatalf("ReorderTracks error = %v, want %v", err, ports.ErrPlaylistChangedDuringReorder)
	}
	assertPositionsAreContiguous(ctx, t, pool, pl.ID, 3)
}

// TestPgxPlaylistRepo_ReorderTracks_RefusesAPlanStaleFromARemove is the same
// race without the add: writing the stale plan raises no constraint at all,
// it renumbers the survivors around the slot the removed track left and the
// playlist keeps a permanent gap while the request reports success.
func TestPgxPlaylistRepo_ReorderTracks_RefusesAPlanStaleFromARemove(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	pl, ids := seedPlaylistWithTracks(ctx, t, pool, userId, 3)
	plan := positionPlan(ids)

	if _, err := repo.RemoveTrack(ctx, userId, pl.ID, ids[0]); err != nil {
		t.Fatalf("RemoveTrack: %v", err)
	}

	err := repo.ReorderTracks(ctx, userId, pl.ID, plan)

	if !errors.Is(err, ports.ErrPlaylistChangedDuringReorder) {
		t.Fatalf("ReorderTracks error = %v, want %v", err, ports.ErrPlaylistChangedDuringReorder)
	}
	assertPositionsAreContiguous(ctx, t, pool, pl.ID, 2)
}

// TestPgxPlaylistRepo_ReorderTracks_AcceptsAPlanForAnOverCapPlaylist holds the
// other side of the staleness check: the caller plans from GetTrackOrder, which
// stops at the read cap, so on a longer playlist the plan names fewer tracks
// than the playlist holds. That is the read the caller was given, not a stale
// one, and the reorder must go through.
func TestPgxPlaylistRepo_ReorderTracks_AcceptsAPlanForAnOverCapPlaylist(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	// Seeded first: the same bound caps adds, so the only over-cap playlists
	// are the ones that grew while it was higher.
	pl, ids := seedPlaylistWithTracks(ctx, t, pool, userId, 5)
	withPlaylistTrackCap(t, 3)

	capped, _, err := repo.GetTrackOrder(ctx, pl.ID, userId)
	if err != nil {
		t.Fatalf("GetTrackOrder: %v", err)
	}
	reversed := []domain.TrackId{capped[2], capped[1], capped[0]}

	if err := repo.ReorderTracks(ctx, userId, pl.ID, positionPlan(reversed)); err != nil {
		t.Fatalf("ReorderTracks: %v", err)
	}
	want := []domain.TrackId{ids[2], ids[1], ids[0], ids[3], ids[4]}
	if got := fetchOrder(ctx, t, pool, pl.ID); !sameOrder(got, want) {
		t.Fatalf("order after reorder = %v, want %v", got, want)
	}
}

// addTrackToPlaylist appends a freshly added track, standing in for the
// concurrent add of another request.
func addTrackToPlaylist(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userId shared.UserId, playlistId domain.PlaylistId) {
	t.Helper()
	tr := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, tr.ID, userId)
	if _, _, err := NewPgxTrackRepository(pool).Add(ctx, tr); err != nil {
		t.Fatalf("Add track: %v", err)
	}
	if err := NewPgxPlaylistRepository(pool).AddTrack(ctx, userId, playlistId, tr.ID); err != nil {
		t.Fatalf("AddTrack: %v", err)
	}
}

// TestPgxPlaylistRepo_AddsRefuseAVanishedTrack reproduces issue #2197's add
// race: the service confirms the caller owns the track, the track is deleted,
// and the insert then breaks playlist_tracks' foreign key. A raw 23503 reaches
// the client as a 500, so both add paths must report the track as missing.
func TestPgxPlaylistRepo_AddsRefuseAVanishedTrack(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	pl := newTestPlaylistForDB(t, userId)
	cleanupPlaylist(t, pool, pl.ID, userId)
	if err := repo.Create(ctx, pl); err != nil {
		t.Fatalf("Create playlist: %v", err)
	}
	vanished := vanishedTrackId(ctx, t, pool, userId)

	adds := []struct {
		name string
		add  func() error
	}{
		{"AddTrack", func() error { return repo.AddTrack(ctx, userId, pl.ID, vanished) }},
		{"AddTracks", func() error {
			_, err := repo.AddTracks(ctx, userId, pl.ID, []domain.TrackId{vanished})
			return err
		}},
	}
	for _, tt := range adds {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.add()

			if !errors.Is(err, ports.ErrTrackMissing) {
				t.Fatalf("error = %v, want %v", err, ports.ErrTrackMissing)
			}
		})
	}
}

// vanishedTrackId returns the id of a track that existed a moment ago, the
// state the caller's ownership lookup leaves behind when a delete wins the
// race to the insert.
func vanishedTrackId(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userId shared.UserId) domain.TrackId {
	t.Helper()
	tr := newTestTrackForDB(t, userId)
	if _, _, err := NewPgxTrackRepository(pool).Add(ctx, tr); err != nil {
		t.Fatalf("Add track: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM tracks WHERE id = $1`, tr.ID.UUID()); err != nil {
		t.Fatalf("delete track: %v", err)
	}
	return tr.ID
}

// TestPgxPlaylistRepo_Update_RefusesAVanishedPlaylist reproduces issue #2197's
// rename race: the playlist is deleted between the read and the write, the
// UPDATE matches no row, and an Update that ignores RowsAffected calls that
// success — so the request answers 200 and publishes a rename of a playlist
// nobody can read.
func TestPgxPlaylistRepo_Update_RefusesAVanishedPlaylist(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	pl := newTestPlaylistForDB(t, userId)
	cleanupPlaylist(t, pool, pl.ID, userId)
	if err := repo.Create(ctx, pl); err != nil {
		t.Fatalf("Create playlist: %v", err)
	}
	if _, err := repo.Delete(ctx, pl.ID, userId); err != nil {
		t.Fatalf("Delete playlist: %v", err)
	}

	renamed := *pl
	renamed.Name = "Renamed"
	err := repo.Update(ctx, &renamed)

	if !errors.Is(err, ports.ErrPlaylistNotOwned) {
		t.Fatalf("Update error = %v, want %v", err, ports.ErrPlaylistNotOwned)
	}
	got, _, err := repo.GetByID(ctx, pl.ID, userId)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got != nil {
		t.Fatalf("GetByID returned %v, want nil: the update resurrected a deleted playlist", got)
	}
}

// TestPgxPlaylistRepo_Update_RefusesAnotherUsersPlaylist holds the other half
// of the owner-scoped write: a rename aimed at a playlist the caller does not
// own must be refused by the data layer, not silently write nothing.
func TestPgxPlaylistRepo_Update_RefusesAnotherUsersPlaylist(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	owner := shared.NewUserId(uuid.New())
	stranger := shared.NewUserId(uuid.New())

	pl := newTestPlaylistForDB(t, owner)
	cleanupPlaylist(t, pool, pl.ID, owner)
	if err := repo.Create(ctx, pl); err != nil {
		t.Fatalf("Create playlist: %v", err)
	}

	stolen := *pl
	stolen.UserId = stranger
	stolen.Name = "Renamed"
	err := repo.Update(ctx, &stolen)

	if !errors.Is(err, ports.ErrPlaylistNotOwned) {
		t.Fatalf("Update error = %v, want %v", err, ports.ErrPlaylistNotOwned)
	}
	got, _, err := repo.GetByID(ctx, pl.ID, owner)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil || got.Name != pl.Name {
		t.Fatalf("name after refused rename = %v, want %q", got, pl.Name)
	}
}
