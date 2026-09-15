package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// rowCounter is a pgx tracer that tallies, per statement, how many rows each
// query read (SELECT) or wrote (INSERT/UPDATE/DELETE), including statements
// sent in a batch. It lets the tests below assert the cost of a membership
// mutation against a real database instead of trusting the SQL text.
type rowCounter struct {
	mu      sync.Mutex
	maxRead int
	written int
}

func (c *rowCounter) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxRead, c.written = 0, 0
}

// snapshot returns the most rows any single SELECT returned and the total rows
// written since the last reset.
func (c *rowCounter) snapshot() (maxRead, written int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.maxRead, c.written
}

func (c *rowCounter) record(tag pgconn.CommandTag, err error) {
	if err != nil {
		return
	}
	n := int(tag.RowsAffected())
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case tag.Select():
		if n > c.maxRead {
			c.maxRead = n
		}
	case tag.Insert(), tag.Update(), tag.Delete():
		c.written += n
	}
}

func (c *rowCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}

func (c *rowCounter) TraceQueryEnd(_ context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	c.record(data.CommandTag, data.Err)
}

func (c *rowCounter) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	return ctx
}

func (c *rowCounter) TraceBatchQuery(_ context.Context, _ *pgx.Conn, data pgx.TraceBatchQueryData) {
	c.record(data.CommandTag, data.Err)
}

func (c *rowCounter) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData) {}

// rowCountingPool opens a pool on DATABASE_URL whose every statement is tallied by
// the returned rowCounter.
func rowCountingPool(t *testing.T) (*pgxpool.Pool, *rowCounter) {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	counter := &rowCounter{}
	cfg.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, counter
}

// assertContiguousOrder checks the playlist holds exactly want, in order, at
// positions 0..len(want)-1.
func assertContiguousOrder(ctx context.Context, t *testing.T, pool *pgxpool.Pool, playlistId domain.PlaylistId, want []domain.TrackId) {
	t.Helper()
	if got := fetchOrder(ctx, t, pool, playlistId); !sameOrder(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	positions := fetchPositions(ctx, t, pool, playlistId)
	for i, p := range positions {
		if p != i {
			t.Fatalf("positions are not contiguous: %v", positions)
		}
	}
}

func without(ids []domain.TrackId, drop ...domain.TrackId) []domain.TrackId {
	skip := make(map[domain.TrackId]bool, len(drop))
	for _, id := range drop {
		skip[id] = true
	}
	out := make([]domain.TrackId, 0, len(ids))
	for _, id := range ids {
		if !skip[id] {
			out = append(out, id)
		}
	}
	return out
}

// TestPlaylistMembership_SingleTrackMutationCost_IsBoundedByTheChange drives the
// real membership service over the real repository on a playlist of n tracks
// (issue #1061). A single-track add or remove must not read the playlist's
// track list, a removal must rewrite only the tail behind the removed slot,
// and a reorder must rewrite only the rows whose position actually changed.
// Before the fix every call loaded all n rows and removals/reorders rewrote
// every remaining row, so each of these bounds failed.
func TestPlaylistMembership_SingleTrackMutationCost_IsBoundedByTheChange(t *testing.T) {
	pool := testPool(t)
	countedPool, counter := rowCountingPool(t)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	const n = 40
	pl, ids := seedPlaylistWithTracks(ctx, t, pool, userId, n)
	svc := service.NewPlaylistMembershipService(NewPgxPlaylistRepository(countedPool), NewPgxTrackRepository(pool))
	trackRepo := NewPgxTrackRepository(pool)

	// newTrack registers its cleanup on the parent test: a subtest-scoped
	// cleanup would delete the track (and, by cascade, its membership) before
	// the next subtest runs.
	parent := t
	newTrack := func(t *testing.T) domain.TrackId {
		t.Helper()
		tr := newTestTrackForDB(t, userId)
		cleanupTrack(parent, pool, tr.ID, userId)
		if _, _, err := trackRepo.Add(ctx, tr); err != nil {
			t.Fatalf("Add track: %v", err)
		}
		return tr.ID
	}
	order := append([]domain.TrackId(nil), ids...)

	t.Run("AddTrack reads no track list and writes one row", func(t *testing.T) {
		fresh := newTrack(t)
		counter.reset()
		if err := svc.AddTrack(ctx, userId, pl.ID, fresh); err != nil {
			t.Fatalf("AddTrack: %v", err)
		}
		maxRead, written := counter.snapshot()
		if maxRead > 1 || written != 1 {
			t.Fatalf("AddTrack on %d-track playlist: max rows read by one statement = %d (want <= 1), rows written = %d (want 1)", len(order), maxRead, written)
		}
		order = append(order, fresh)
		assertContiguousOrder(ctx, t, pool, pl.ID, order)
	})

	t.Run("AddTrack of a member reads no track list and writes nothing", func(t *testing.T) {
		counter.reset()
		err := svc.AddTrack(ctx, userId, pl.ID, order[3])
		if !errors.Is(err, domain.ErrTrackAlreadyInPlaylist) {
			t.Fatalf("AddTrack(duplicate) err = %v, want ErrTrackAlreadyInPlaylist", err)
		}
		maxRead, written := counter.snapshot()
		if maxRead > 1 || written != 0 {
			t.Fatalf("duplicate AddTrack: max rows read = %d (want <= 1), rows written = %d (want 0)", maxRead, written)
		}
		assertContiguousOrder(ctx, t, pool, pl.ID, order)
	})

	t.Run("RemoveTrack rewrites only the tail", func(t *testing.T) {
		victim := order[len(order)-4]
		counter.reset()
		if err := svc.RemoveTrack(ctx, userId, pl.ID, victim); err != nil {
			t.Fatalf("RemoveTrack: %v", err)
		}
		maxRead, written := counter.snapshot()
		// One DELETE plus the three rows behind it shifting up one slot.
		if maxRead > 1 || written != 4 {
			t.Fatalf("RemoveTrack near the end of a %d-track playlist: max rows read = %d (want <= 1), rows written = %d (want 4)", len(order), maxRead, written)
		}
		order = without(order, victim)
		assertContiguousOrder(ctx, t, pool, pl.ID, order)
	})

	t.Run("RemoveTrack of a non-member writes nothing", func(t *testing.T) {
		counter.reset()
		if err := svc.RemoveTrack(ctx, userId, pl.ID, domain.NewTrackId()); err != nil {
			t.Fatalf("RemoveTrack(non-member): %v", err)
		}
		maxRead, written := counter.snapshot()
		if maxRead > 1 || written != 0 {
			t.Fatalf("RemoveTrack(non-member): max rows read = %d (want <= 1), rows written = %d (want 0)", maxRead, written)
		}
		assertContiguousOrder(ctx, t, pool, pl.ID, order)
	})

	t.Run("RemoveTracks rewrites only the tail behind the first removed slot", func(t *testing.T) {
		last := len(order) - 1
		first, second := order[last-6], order[last-2]
		counter.reset()
		removed, err := svc.RemoveTracks(ctx, userId, pl.ID, []domain.TrackId{second, first, domain.NewTrackId(), second})
		if err != nil {
			t.Fatalf("RemoveTracks: %v", err)
		}
		if removed != 2 {
			t.Fatalf("removed = %d, want 2", removed)
		}
		maxRead, written := counter.snapshot()
		// Two DELETEs plus the five surviving rows behind the first removed slot.
		if maxRead > 2 || written != 7 {
			t.Fatalf("RemoveTracks near the end of a %d-track playlist: max rows read = %d (want <= 2), rows written = %d (want 7)", len(order), maxRead, written)
		}
		order = without(order, first, second)
		assertContiguousOrder(ctx, t, pool, pl.ID, order)
	})

	t.Run("Reorder rewrites only the moved rows", func(t *testing.T) {
		reordered := append([]domain.TrackId(nil), order...)
		reordered[5], reordered[6] = reordered[6], reordered[5]
		counter.reset()
		if err := svc.Reorder(ctx, userId, pl.ID, reordered); err != nil {
			t.Fatalf("Reorder: %v", err)
		}
		_, written := counter.snapshot()
		if written != 2 {
			t.Fatalf("Reorder swapping two tracks of %d: rows written = %d, want 2", len(order), written)
		}
		order = reordered
		assertContiguousOrder(ctx, t, pool, pl.ID, order)
	})

	t.Run("AddTracks reads no track list and appends only new tracks", func(t *testing.T) {
		a, b := newTrack(t), newTrack(t)
		counter.reset()
		added, err := svc.AddTracks(ctx, userId, pl.ID, []domain.TrackId{a, order[0], a, b})
		if err != nil {
			t.Fatalf("AddTracks: %v", err)
		}
		if added != 2 {
			t.Fatalf("added = %d, want 2", added)
		}
		maxRead, written := counter.snapshot()
		if maxRead > 2 || written != 2 {
			t.Fatalf("AddTracks on a %d-track playlist: max rows read = %d (want <= 2), rows written = %d (want 2)", len(order), maxRead, written)
		}
		order = append(order, a, b)
		assertContiguousOrder(ctx, t, pool, pl.ID, order)
	})
}

// TestPgxPlaylistRepo_ConcurrentDuplicateAddTrack_OneWins races the same track
// into a playlist from many callers. Membership is decided inside the locked
// insert, so exactly one caller adds it and every other gets
// domain.ErrTrackAlreadyInPlaylist, never a primary-key violation.
func TestPgxPlaylistRepo_ConcurrentDuplicateAddTrack_OneWins(t *testing.T) {
	pool := testPool(t)
	playlistRepo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	pl, ids := seedPlaylistWithTracks(ctx, t, pool, userId, 1)
	tr := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, tr.ID, userId)
	if _, _, err := NewPgxTrackRepository(pool).Add(ctx, tr); err != nil {
		t.Fatalf("Add track: %v", err)
	}

	const callers = 8
	errs := make([]error, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = playlistRepo.AddTrack(ctx, userId, pl.ID, tr.ID)
		}(i)
	}
	close(start)
	wg.Wait()

	wins := 0
	for i, err := range errs {
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, domain.ErrTrackAlreadyInPlaylist):
			t.Fatalf("caller %d: err = %v, want nil or ErrTrackAlreadyInPlaylist", i, err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d callers added the track, want exactly 1", wins)
	}
	assertContiguousOrder(ctx, t, pool, pl.ID, []domain.TrackId{ids[0], tr.ID})
}

// TestPgxPlaylistRepo_RemoveTracks_KeepsOrderAcrossGaps removes tracks from a
// playlist whose positions already have gaps (a library track deletion
// cascades out of playlist_tracks without renumbering). The tail-only shift must
// keep the survivors' relative order and never collide on a position.
func TestPgxPlaylistRepo_RemoveTracks_KeepsOrderAcrossGaps(t *testing.T) {
	pool := testPool(t)
	playlistRepo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	pl, ids := seedPlaylistWithTracks(ctx, t, pool, userId, 8)
	if _, err := pool.Exec(ctx,
		`DELETE FROM playlist_tracks WHERE playlist_id = $1 AND track_id = ANY($2)`,
		pl.ID.UUID(), []uuid.UUID{ids[1].UUID(), ids[4].UUID()},
	); err != nil {
		t.Fatalf("punch gaps: %v", err)
	}

	removed, err := playlistRepo.RemoveTracks(ctx, userId, pl.ID, []domain.TrackId{ids[5], ids[2], ids[1], ids[5]})
	if err != nil {
		t.Fatalf("RemoveTracks: %v", err)
	}
	if want := []domain.TrackId{ids[5], ids[2]}; !reflect.DeepEqual(removed, want) {
		t.Fatalf("removed = %v, want %v", removed, want)
	}
	want := []domain.TrackId{ids[0], ids[3], ids[6], ids[7]}
	if got := fetchOrder(ctx, t, pool, pl.ID); !sameOrder(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}

	gone, err := playlistRepo.RemoveTrack(ctx, userId, pl.ID, ids[3])
	if err != nil || !gone {
		t.Fatalf("RemoveTrack(member) = %v, %v; want true, nil", gone, err)
	}
	want = []domain.TrackId{ids[0], ids[6], ids[7]}
	if got := fetchOrder(ctx, t, pool, pl.ID); !sameOrder(got, want) {
		t.Fatalf("order after RemoveTrack = %v, want %v", got, want)
	}
}

// TestPgxPlaylistRepo_MembershipReads_AreOwnerScoped covers the two targeted
// reads the membership service relies on instead of GetWithTracks.
func TestPgxPlaylistRepo_MembershipReads_AreOwnerScoped(t *testing.T) {
	pool := testPool(t)
	playlistRepo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	owner := shared.NewUserId(uuid.New())
	stranger := shared.NewUserId(uuid.New())

	pl, ids := seedPlaylistWithTracks(ctx, t, pool, owner, 3)
	empty := newTestPlaylistForDB(t, owner)
	cleanupPlaylist(t, pool, empty.ID, owner)
	if err := playlistRepo.Create(ctx, empty); err != nil {
		t.Fatalf("Create empty playlist: %v", err)
	}

	cases := []struct {
		name      string
		id        domain.PlaylistId
		user      shared.UserId
		wantFound bool
		wantOrder []domain.TrackId
	}{
		{"owned", pl.ID, owner, true, ids},
		{"owned and empty", empty.ID, owner, true, []domain.TrackId{}},
		{"foreign", pl.ID, stranger, false, nil},
		{"missing", domain.NewPlaylistId(), owner, false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exists, err := playlistRepo.Exists(ctx, tc.id, tc.user)
			if err != nil || exists != tc.wantFound {
				t.Fatalf("Exists = %v, %v; want %v, nil", exists, err, tc.wantFound)
			}
			order, found, err := playlistRepo.GetTrackOrder(ctx, tc.id, tc.user)
			if err != nil || found != tc.wantFound {
				t.Fatalf("GetTrackOrder found = %v, err = %v; want %v, nil", found, err, tc.wantFound)
			}
			if tc.wantFound && !reflect.DeepEqual(order, tc.wantOrder) {
				t.Fatalf("GetTrackOrder = %v, want %v", order, tc.wantOrder)
			}
		})
	}
}
