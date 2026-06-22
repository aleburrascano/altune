package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedTrackRow inserts a minimal tracks row (the columns FindRelatedBy* reads,
// plus the NOT NULL set) and registers cleanup. Discovery touching catalog's
// tracks table here is the deliberate, documented read coupling (ADR-0012-era
// review, candidate #2).
func seedTrackRow(t *testing.T, pool *pgxpool.Pool, userId uuid.UUID, title, artist, album string) {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, album, added_at, acquisition_status, dedup_key)
		 VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7)`,
		id, userId, title, artist, album, time.Now().UTC(), id.String(),
	)
	if err != nil {
		t.Fatalf("seed track: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id = $1`, id)
	})
}

func TestPgxRelationshipQuerier_FindRelated(t *testing.T) {
	pool := testPool(t)
	q := NewPgxRelationshipQuerier(pool)
	ctx := context.Background()

	userA := uuid.New()
	userB := uuid.New()
	// Unique album/artist so assertions are isolated from real dev data.
	suffix := uuid.New().String()[:8]
	album := "Relink Album " + suffix
	artist := "Relink Artist " + suffix

	// Same title+artist across two users must collapse to one row (DISTINCT ON),
	// and another user's track must surface (the deliberate cross-user read).
	seedTrackRow(t, pool, userA, "Song One", artist, album)
	seedTrackRow(t, pool, userB, "Song One", artist, album)
	seedTrackRow(t, pool, userA, "Song Two", artist, album)
	seedTrackRow(t, pool, userA, "Song Three", artist, "Other "+album)

	t.Run("by album dedups cross-user and scopes to the album", func(t *testing.T) {
		got, err := q.FindRelatedByAlbum(ctx, album, 10)
		if err != nil {
			t.Fatalf("FindRelatedByAlbum: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d matches, want 2 (Song One collapsed + Song Two)", len(got))
		}
	})

	t.Run("by artist spans albums", func(t *testing.T) {
		got, err := q.FindRelatedByArtist(ctx, artist, 10)
		if err != nil {
			t.Fatalf("FindRelatedByArtist: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("got %d matches, want 3 (Song One/Two/Three)", len(got))
		}
	})

	t.Run("limit is honored", func(t *testing.T) {
		got, err := q.FindRelatedByArtist(ctx, artist, 1)
		if err != nil {
			t.Fatalf("FindRelatedByArtist: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d matches, want 1 (limit)", len(got))
		}
	})
}
