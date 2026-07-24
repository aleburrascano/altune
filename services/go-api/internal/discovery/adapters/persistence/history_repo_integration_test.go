//go:build integration

package persistence

import (
	"context"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

func seedHistoryEntry(t *testing.T, repo *PgxSearchHistoryRepository, userId shared.UserId, query string, at time.Time) {
	t.Helper()
	entry := &domain.SearchHistoryEntry{
		ID:         uuid.New(),
		UserId:     userId,
		Query:      query,
		QueryNorm:  query,
		ExecutedAt: at,
	}
	if err := repo.Insert(context.Background(), entry); err != nil {
		t.Fatalf("Insert(%q): %v", query, err)
	}
}

// DeleteAllForUser wipes exactly one user's history; another user's survives.
func TestPgxSearchHistoryRepo_DeleteAllForUser(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxSearchHistoryRepository(pool)
	ctx := context.Background()
	userA := shared.NewUserId(uuid.New())
	userB := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		for _, u := range []shared.UserId{userA, userB} {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM discovery_search_history WHERE user_id = $1`, u.UUID())
		}
	})

	suffix := uuid.New().String()[:8]
	now := time.Now().UTC()
	seedHistoryEntry(t, repo, userA, "hist a1 "+suffix, now.Add(-3*time.Minute))
	seedHistoryEntry(t, repo, userA, "hist a2 "+suffix, now.Add(-2*time.Minute))
	seedHistoryEntry(t, repo, userA, "hist a3 "+suffix, now.Add(-1*time.Minute))
	seedHistoryEntry(t, repo, userB, "hist b1 "+suffix, now.Add(-1*time.Minute))

	if err := repo.DeleteAllForUser(ctx, userA); err != nil {
		t.Fatalf("DeleteAllForUser: %v", err)
	}

	gotA, err := repo.ListDistinctRecent(ctx, userA, 10)
	if err != nil {
		t.Fatalf("ListDistinctRecent(userA): %v", err)
	}
	if len(gotA) != 0 {
		t.Errorf("userA entries after delete = %d, want 0", len(gotA))
	}

	gotB, err := repo.ListDistinctRecent(ctx, userB, 10)
	if err != nil {
		t.Fatalf("ListDistinctRecent(userB): %v", err)
	}
	if len(gotB) != 1 {
		t.Errorf("userB entries after userA delete = %d, want 1 (scoped delete)", len(gotB))
	}

	// Deleting an already-empty history is a no-op, not an error.
	if err := repo.DeleteAllForUser(ctx, userA); err != nil {
		t.Errorf("DeleteAllForUser on empty history: %v, want nil", err)
	}
}

// ListDistinctRecent's limit applies to DISTINCT query_norms, most recent first.
func TestPgxSearchHistoryRepo_ListDistinctRecent_LimitBoundary(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxSearchHistoryRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM discovery_search_history WHERE user_id = $1`, userId.UUID())
	})

	suffix := uuid.New().String()[:8]
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		seedHistoryEntry(t, repo, userId, "limit q"+string(rune('a'+i))+" "+suffix,
			now.Add(time.Duration(i)*time.Second))
	}

	got, err := repo.ListDistinctRecent(ctx, userId, 2)
	if err != nil {
		t.Fatalf("ListDistinctRecent: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (limit)", len(got))
	}
	// The two MOST RECENT norms survive the limit; the oldest is cut.
	if got[0].QueryNorm != "limit qc "+suffix || got[1].QueryNorm != "limit qb "+suffix {
		t.Errorf("got [%q, %q], want the two most recent [%q, %q]",
			got[0].QueryNorm, got[1].QueryNorm, "limit qc "+suffix, "limit qb "+suffix)
	}
}
