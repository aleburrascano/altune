package persistence

import (
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"testing"
	"time"

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
		got, err := q.FindRelatedByAlbumArtist(ctx, callerA, album, artist, 10)
		if err != nil {
			t.Fatalf("FindRelatedByAlbumArtist: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d matches, want 2 (Song One collapsed + Song Two)", len(got))
		}
	})

	t.Run("never returns another user's library tracks", func(t *testing.T) {
		seedTrackRow(t, pool, userB, "B Own Song", artist, album)
		callerB := shared.NewUserId(userB)

		byAlbum, err := q.FindRelatedByAlbumArtist(ctx, callerB, album, artist, 10)
		if err != nil {
			t.Fatalf("FindRelatedByAlbumArtist: %v", err)
		}
		titles := matchTitles(byAlbum)
		for _, leaked := range []string{"Song One", "Song Two", "Song Three"} {
			if titles[leaked] {
				t.Errorf("user B got user A's track %q", leaked)
			}
		}
		if len(byAlbum) != 1 || !titles["B Own Song"] {
			t.Errorf("got %v, want only user B's own track", titles)
		}
	})
}

func TestPgxRelationshipQuerier_FindRelatedByAlbumArtist(t *testing.T) {
	pool := testPool(t)
	q := NewPgxRelationshipQuerier(pool)
	ctx := context.Background()
	user := uuid.New()
	caller := shared.NewUserId(user)
	album := "Greatest Hits " + uuid.New().String()[:8]

	seedTrackRow(t, pool, user, "Bohemian Rhapsody", "Queen", album)
	seedTrackRow(t, pool, user, "Dancing Queen", "ABBA", album)

	got, err := q.FindRelatedByAlbumArtist(ctx, caller, album, "Queen", 10)
	if err != nil {
		t.Fatalf("FindRelatedByAlbumArtist: %v", err)
	}
	titles := matchTitles(got)
	if !titles["Bohemian Rhapsody"] || titles["Dancing Queen"] {
		t.Errorf("got %v, want only Queen's track", titles)
	}
}

func TestKeepArtistMatches(t *testing.T) {
	candidates := []albumCandidate{
		{match: ports.RelatedTrackMatch{Title: "Queen Song", Artist: "Queen"}},
		{match: ports.RelatedTrackMatch{Title: "ABBA Song", Artist: "ABBA"}},
		{match: ports.RelatedTrackMatch{Title: "Compilation Song", Artist: "Someone"}, albumArtist: "queen"},
	}
	titlesFor := func(artist string) map[string]bool {
		return matchTitles(keepArtistMatches(candidates, artist, 10))
	}

	if got := titlesFor("Queen"); len(got) != 2 || !got["Queen Song"] || !got["Compilation Song"] {
		t.Errorf("Queen: got %v, want Queen Song and the album-artist Compilation Song", got)
	}
	if got := titlesFor("ABBA"); len(got) != 1 || !got["ABBA Song"] {
		t.Errorf("ABBA: got %v, want only ABBA Song", got)
	}
	if got := titlesFor(""); len(got) != 3 {
		t.Errorf("empty artist: got %v, want album-only matching", got)
	}
}
