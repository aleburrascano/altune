package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func keyed(t *testing.T, userId shared.UserId, key string) *domain.Track {
	t.Helper()
	track := newTestTrackForDB(t, userId)
	track.IdempotencyKey = &key
	return track
}

// TestPgxTrackRepo_ConcurrentAddSameKey asserts that many genuinely concurrent
// creates carrying the same idempotency key — but distinct content and ids —
// collapse to exactly one library row, and every caller receives that one row
// (created reported for exactly one of them). This is the two-concurrent-clients
// failure mode from #698: without the (user_id, idempotency_key) partial unique
// index + ON CONFLICT DO NOTHING, each goroutine would insert its own row.
func TestPgxTrackRepo_ConcurrentAddSameKey(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	key := "idem-" + uuid.NewString()

	const concurrent = 8
	tracks := make([]*domain.Track, concurrent)
	for i := range tracks {
		tr := keyed(t, userId, key)
		cleanupTrack(t, pool, tr.ID, userId)
		tracks[i] = tr
	}

	start := make(chan struct{})
	stored := make([]*domain.Track, concurrent)
	createdFlags := make([]bool, concurrent)
	errs := make([]error, concurrent)
	var wg sync.WaitGroup
	for i, tr := range tracks {
		wg.Add(1)
		go func(i int, tr *domain.Track) {
			defer wg.Done()
			<-start
			stored[i], createdFlags[i], errs[i] = repo.Add(ctx, tr)
		}(i, tr)
	}
	close(start)
	wg.Wait()

	createdCount := 0
	var winnerID domain.TrackId
	for i := range tracks {
		if errs[i] != nil {
			t.Fatalf("concurrent Add %d: %v", i, errs[i])
		}
		if stored[i] == nil {
			t.Fatalf("Add %d returned nil track", i)
		}
		if createdFlags[i] {
			createdCount++
			winnerID = stored[i].ID
		}
	}
	if createdCount != 1 {
		t.Fatalf("created count = %d, want exactly 1 (the key must collapse concurrent creates)", createdCount)
	}
	for i := range stored {
		if stored[i].ID != winnerID {
			t.Fatalf("Add %d returned id %s, want the single winning row %s", i, stored[i].ID, winnerID)
		}
	}

	if got := countTracksForUser(ctx, t, pool, userId); got != 1 {
		t.Fatalf("row count for user = %d, want 1", got)
	}
}

// TestPgxTrackRepo_AddSameKeyAfterCommit asserts that replaying the same
// idempotency key after the first create has committed returns the stored row
// instead of creating a second — the dropped-response-retry failure mode from
// #698. The retry carries a fresh track id and even different content; the
// stored row (the first one) must win.
func TestPgxTrackRepo_AddSameKeyAfterCommit(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	key := "idem-" + uuid.NewString()

	first := keyed(t, userId, key)
	cleanupTrack(t, pool, first.ID, userId)
	firstStored, created, err := repo.Add(ctx, first)
	if err != nil {
		t.Fatalf("first Add: %v", err)
	}
	if !created {
		t.Fatalf("first Add created = false, want true")
	}

	retry := keyed(t, userId, key) // fresh id, different title/artist, same key
	cleanupTrack(t, pool, retry.ID, userId)
	retryStored, created, err := repo.Add(ctx, retry)
	if err != nil {
		t.Fatalf("retry Add: %v", err)
	}
	if created {
		t.Fatalf("retry Add created = true, want false (the committed row must be returned)")
	}
	if retryStored == nil || retryStored.ID != firstStored.ID {
		t.Fatalf("retry returned %v, want the first stored row %s", retryStored, firstStored.ID)
	}

	if got := countTracksForUser(ctx, t, pool, userId); got != 1 {
		t.Fatalf("row count for user = %d, want 1", got)
	}
}

func countTracksForUser(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userId shared.UserId) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE user_id = $1`, userId.UUID()).Scan(&n); err != nil {
		t.Fatalf("count tracks: %v", err)
	}
	return n
}
