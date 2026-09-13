package persistence

import (
	"context"
	"testing"
	"time"

	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

func matchTitles(ms []ports.RelatedTrackMatch) map[string]bool {
	out := make(map[string]bool, len(ms))
	for _, m := range ms {
		out[m.Title] = true
	}
	return out
}

func TestPgxRelationshipQuerier_FindRelated(t *testing.T) {
	pool := testPool(t)
	q := NewPgxRelationshipQuerier(pool)
	ctx := context.Background()

	userA := uuid.New()
	userB := uuid.New()
	callerA := shared.NewUserId(userA)
	suffix := uuid.New().String()[:8]
	album := "Relink Album " + suffix
	artist := "Relink Artist " + suffix

	seedTrackRow(t, pool, userA, "Song One", artist, album)
	seedTrackRow(t, pool, userA, "Song One", artist, album)
	seedTrackRow(t, pool, userA, "Song Two", artist, album)
	seedTrackRow(t, pool, userA, "Song Three", artist, "Other "+album)

	t.Run("by album dedups and scopes to the album", func(t *testing.T) {
		got, err := q.FindRelatedByAlbum(ctx, callerA, album, 10)
		if err != nil {
			t.Fatalf("FindRelatedByAlbum: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d matches, want 2 (Song One collapsed + Song Two)", len(got))
		}
	})

	t.Run("by artist spans albums", func(t *testing.T) {
		got, err := q.FindRelatedByArtist(ctx, callerA, artist, 10)
		if err != nil {
			t.Fatalf("FindRelatedByArtist: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("got %d matches, want 3 (Song One/Two/Three)", len(got))
		}
	})

	t.Run("limit is honored", func(t *testing.T) {
		got, err := q.FindRelatedByArtist(ctx, callerA, artist, 1)
		if err != nil {
			t.Fatalf("FindRelatedByArtist: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d matches, want 1 (limit)", len(got))
		}
	})

	// Regression for #570: the lookup must be scoped to the caller's library in
	// the query, so user B never sees user A's private tracks.
	t.Run("never returns another user's library tracks", func(t *testing.T) {
		seedTrackRow(t, pool, userB, "B Own Song", artist, album)
		callerB := shared.NewUserId(userB)

		byAlbum, err := q.FindRelatedByAlbum(ctx, callerB, album, 10)
		if err != nil {
			t.Fatalf("FindRelatedByAlbum: %v", err)
		}
		byArtist, err := q.FindRelatedByArtist(ctx, callerB, artist, 10)
		if err != nil {
			t.Fatalf("FindRelatedByArtist: %v", err)
		}
		for name, got := range map[string][]ports.RelatedTrackMatch{"album": byAlbum, "artist": byArtist} {
			titles := matchTitles(got)
			for _, leaked := range []string{"Song One", "Song Two", "Song Three"} {
				if titles[leaked] {
					t.Errorf("by %s: user B got user A's track %q", name, leaked)
				}
			}
			if len(got) != 1 || !titles["B Own Song"] {
				t.Errorf("by %s: got %v, want only user B's own track", name, titles)
			}
		}
	})
}
