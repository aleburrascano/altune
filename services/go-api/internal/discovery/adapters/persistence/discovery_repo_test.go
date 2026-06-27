package persistence

import (
	"context"
	"os"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func TestPgxSearchHistoryRepo_InsertAndListDistinctRecent(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxSearchHistoryRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	// Arrange: insert 3 entries, 2 with the same query_norm
	entries := []*domain.SearchHistoryEntry{
		{
			ID:        uuid.New(),
			UserId:    userId,
			Query:     "Beatles",
			QueryNorm: "beatles",
			ExecutedAt: time.Now().UTC().Add(-3 * time.Minute),
		},
		{
			ID:        uuid.New(),
			UserId:    userId,
			Query:     "beatles",
			QueryNorm: "beatles",
			ExecutedAt: time.Now().UTC().Add(-1 * time.Minute),
		},
		{
			ID:        uuid.New(),
			UserId:    userId,
			Query:     "Pink Floyd",
			QueryNorm: "pink floyd",
			ExecutedAt: time.Now().UTC().Add(-2 * time.Minute),
		},
	}

	for i, e := range entries {
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM discovery_search_history WHERE id = $1`, e.ID)
		})
		if err := repo.Insert(ctx, e); err != nil {
			t.Fatalf("Insert entry %d: %v", i, err)
		}
	}

	// Act: list distinct recent
	got, err := repo.ListDistinctRecent(ctx, userId, 10)
	if err != nil {
		t.Fatalf("ListDistinctRecent() error = %v", err)
	}

	// Assert: should return 2 distinct query_norms
	if len(got) != 2 {
		t.Fatalf("len(entries) = %d, want 2 (distinct query_norms)", len(got))
	}

	// The "beatles" entry should be the more recent one (1 min ago)
	foundBeatles := false
	for _, e := range got {
		if e.QueryNorm == "beatles" {
			foundBeatles = true
			if e.ID != entries[1].ID {
				t.Errorf("beatles entry ID = %v, want %v (most recent)", e.ID, entries[1].ID)
			}
		}
	}
	if !foundBeatles {
		t.Error("missing 'beatles' entry in distinct recent results")
	}

	// Assert: most recent first
	if got[0].ExecutedAt.Before(got[1].ExecutedAt) {
		t.Error("entries not in descending executed_at order")
	}
}

func TestPgxSearchHistoryRepo_TrimToN(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxSearchHistoryRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	// Arrange: insert 5 entries
	ids := make([]uuid.UUID, 5)
	for i := 0; i < 5; i++ {
		ids[i] = uuid.New()
		entry := &domain.SearchHistoryEntry{
			ID:         ids[i],
			UserId:     userId,
			Query:      "query-" + ids[i].String()[:8],
			QueryNorm:  "query" + ids[i].String()[:8],
			ExecutedAt: time.Now().UTC().Add(time.Duration(i) * time.Second),
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM discovery_search_history WHERE id = $1`, entry.ID)
		})
		if err := repo.Insert(ctx, entry); err != nil {
			t.Fatalf("Insert entry %d: %v", i, err)
		}
	}

	// Act: trim to 2
	if err := repo.TrimToN(ctx, userId, 2); err != nil {
		t.Fatalf("TrimToN() error = %v", err)
	}

	// Assert: only 2 remain
	got, err := repo.ListDistinctRecent(ctx, userId, 10)
	if err != nil {
		t.Fatalf("ListDistinctRecent after trim: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("entries after TrimToN(2) = %d, want 2", len(got))
	}
}
