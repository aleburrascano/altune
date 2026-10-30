package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newUnreleasedArtist(t *testing.T, pool *pgxpool.Pool, store *PgxUnreleasedStore) ports.UnreleasedArtistKey {
	t.Helper()
	key := ports.UnreleasedArtistKey{Provider: domain.ProviderDeezer, ExternalID: "it-" + uuid.NewString()}
	if err := store.Record(context.Background(), key, "Artist "+key.ExternalID, map[domain.ProviderName]string{domain.ProviderMusicBrainz: "mb"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM unreleased_artists WHERE provider = $1 AND external_id = $2`, key.Provider.String(), key.ExternalID)
	})
	return key
}

func unreleasedTitles(songs []domain.UnreleasedSong) []string {
	out := make([]string, len(songs))
	for i, s := range songs {
		out[i] = s.Title
	}
	return out
}

func TestUnreleasedStoreWrite(t *testing.T) {
	pool := testPool(t)
	store := NewPgxUnreleasedStore(pool)
	ctx := context.Background()
	key := newUnreleasedArtist(t, pool, store)

	if _, _, built, err := store.Read(ctx, key); err != nil || built {
		t.Fatalf("Read before any write: built=%v err=%v", built, err)
	}

	t0 := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	first := []domain.UnreleasedSong{{Title: "A"}, {Title: "B"}, {Title: "C"}}
	if err := store.Write(ctx, key, first, true, t0); err != nil {
		t.Fatalf("Write: %v", err)
	}
	songs, complete, built, err := store.Read(ctx, key)
	if err != nil || !built || !complete || len(songs) != 3 || songs[0].Title != "A" || songs[2].Title != "C" {
		t.Fatalf("Read = %v complete=%v built=%v err=%v", unreleasedTitles(songs), complete, built, err)
	}

	if err := store.Write(ctx, key, []domain.UnreleasedSong{{Title: "Stale"}}, false, t0.Add(-time.Minute)); err != nil {
		t.Fatalf("stale Write: %v", err)
	}
	songs, complete, _, _ = store.Read(ctx, key)
	if len(songs) != 3 || !complete {
		t.Errorf("stale write changed the store: %v complete=%v", unreleasedTitles(songs), complete)
	}

	if err := store.Write(ctx, key, []domain.UnreleasedSong{{Title: "D"}}, false, t0.Add(time.Minute)); err != nil {
		t.Fatalf("newer Write: %v", err)
	}
	songs, complete, _, _ = store.Read(ctx, key)
	if len(songs) != 1 || songs[0].Title != "D" || complete {
		t.Errorf("newer write = %v complete=%v, want [D] incomplete", unreleasedTitles(songs), complete)
	}
}

func TestUnreleasedStoreDue(t *testing.T) {
	pool := testPool(t)
	store := NewPgxUnreleasedStore(pool)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := pool.Exec(ctx, `UPDATE unreleased_artists SET last_requested_at = now() - interval '60 days'`); err != nil {
		t.Fatalf("park existing artists: %v", err)
	}

	seed := func(attempted time.Time, built *time.Time, complete bool, songCount int, requested time.Time) ports.UnreleasedArtistKey {
		key := newUnreleasedArtist(t, pool, store)
		if _, err := pool.Exec(ctx,
			`UPDATE unreleased_artists SET attempted_at = $3, built_at = $4, complete = $5, song_count = $6, last_requested_at = $7
			 WHERE provider = $1 AND external_id = $2`,
			key.Provider.String(), key.ExternalID, attempted, built, complete, songCount, requested); err != nil {
			t.Fatalf("seed: %v", err)
		}
		return key
	}
	ago := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }

	oldest := seed(now.Add(-5*time.Hour), nil, false, 0, now)
	middle := seed(now.Add(-3*time.Hour), ago(200*time.Hour), true, 4, now)
	newest := seed(now.Add(-2*time.Hour), ago(2*time.Hour), false, 0, now)
	seed(now.Add(-10*time.Minute), nil, false, 0, now)
	seed(now.Add(-9*time.Hour), ago(24*time.Hour), true, 4, now)
	seed(now.Add(-9*time.Hour), ago(10*time.Minute), false, 0, now)
	seed(now.Add(-9*time.Hour), nil, false, 0, now.Add(-31*24*time.Hour))

	due, err := store.Due(ctx, now, 2)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if len(due) != 2 || due[0].Key != oldest || due[1].Key != middle {
		t.Fatalf("Due(limit 2) = %+v, want [oldest middle]", due)
	}
	if due[0].KnownIDs[domain.ProviderMusicBrainz] != "mb" {
		t.Errorf("KnownIDs = %v", due[0].KnownIDs)
	}

	due, err = store.Due(ctx, now, 10)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if len(due) != 1 || due[0].Key != newest {
		t.Fatalf("second Due = %+v, want only newest (others just stamped)", due)
	}
}
