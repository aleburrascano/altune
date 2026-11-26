package persistence_test

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/adapters/persistence"
	sharedids "altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/testdb"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPostgresSearchHistoryRepo_InsertAndListDistinctRecent(t *testing.T) {
	pool := testdb.Pool(t)
	repo := persistence.NewPostgresSearchHistoryRepository(pool)
	ctx := context.Background()
	userId := sharedids.NewUserId(uuid.New())

	entries := []*domain.SearchHistoryEntry{
		{
			ID:         sharedids.NewEntityId(),
			UserId:     userId,
			Query:      "Beatles",
			QueryNorm:  "beatles",
			ExecutedAt: time.Now().UTC().Add(-3 * time.Minute),
		},
		{
			ID:         sharedids.NewEntityId(),
			UserId:     userId,
			Query:      "beatles",
			QueryNorm:  "beatles",
			ExecutedAt: time.Now().UTC().Add(-1 * time.Minute),
		},
		{
			ID:         sharedids.NewEntityId(),
			UserId:     userId,
			Query:      "Pink Floyd",
			QueryNorm:  "pink floyd",
			ExecutedAt: time.Now().UTC().Add(-2 * time.Minute),
		},
	}

	for i, e := range entries {
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM discovery_search_history WHERE id = $1`, e.ID.UUID())
		})
		if err := repo.Insert(ctx, e); err != nil {
			t.Fatalf("Insert entry %d: %v", i, err)
		}
	}

	got, err := repo.ListDistinctRecent(ctx, userId, 10)
	if err != nil {
		t.Fatalf("ListDistinctRecent() error = %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("len(entries) = %d, want 2 (distinct query_norms)", len(got))
	}

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

	if got[0].ExecutedAt.Before(got[1].ExecutedAt) {
		t.Error("entries not in descending executed_at order")
	}
}

func TestPostgresSearchHistoryRepo_TrimToN(t *testing.T) {
	pool := testdb.Pool(t)
	repo := persistence.NewPostgresSearchHistoryRepository(pool)
	ctx := context.Background()
	userId := sharedids.NewUserId(uuid.New())

	ids := make([]sharedids.EntityId, 5)
	for i := 0; i < 5; i++ {
		ids[i] = sharedids.NewEntityId()
		entry := &domain.SearchHistoryEntry{
			ID:         ids[i],
			UserId:     userId,
			Query:      "query-" + ids[i].String()[:8],
			QueryNorm:  "query" + ids[i].String()[:8],
			ExecutedAt: time.Now().UTC().Add(time.Duration(i) * time.Second),
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM discovery_search_history WHERE id = $1`, entry.ID.UUID())
		})
		if err := repo.Insert(ctx, entry); err != nil {
			t.Fatalf("Insert entry %d: %v", i, err)
		}
	}

	if err := repo.TrimToN(ctx, userId, 2); err != nil {
		t.Fatalf("TrimToN() error = %v", err)
	}

	got, err := repo.ListDistinctRecent(ctx, userId, 10)
	if err != nil {
		t.Fatalf("ListDistinctRecent after trim: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("entries after TrimToN(2) = %d, want 2", len(got))
	}
}
