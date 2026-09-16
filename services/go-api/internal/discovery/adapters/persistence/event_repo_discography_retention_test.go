package persistence

import (
	"altune/go-api/internal/discovery/ports"
	"context"
	"testing"
	"time"
)

// remainingDiscographyRefs lists the artist_ref of every discography_observed row
// still in the table, so a retention test can assert exactly which rows survived a
// prune rather than inferring it from the windowed read.
func remainingDiscographyRefs(t *testing.T, store *PgxEventStore) map[string]bool {
	t.Helper()
	rows, err := store.pool.Query(context.Background(),
		`SELECT payload->>'artist_ref' FROM discovery_events WHERE event_type = 'discography_observed'`)
	if err != nil {
		t.Fatalf("list remaining rows: %v", err)
	}
	defer rows.Close()
	refs := map[string]bool{}
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			t.Fatalf("scan remaining ref: %v", err)
		}
		refs[ref] = true
	}
	return refs
}

// TestPgxEventStore_PruneDiscographyObserved proves the retention invariant against
// real Postgres: the periodic prune evicts discography_observed rows older than the
// retention window while leaving every in-window row — including one at the widest
// readable aggregate window (365 days) and one exactly at the retention cutoff —
// untouched. It then confirms the capped aggregate read never scans the evicted row,
// so the table and the scan both stay bounded no matter how many opens land.
func TestPgxEventStore_PruneDiscographyObserved(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM discovery_events WHERE event_type = 'discography_observed'`)
	})
	_, _ = pool.Exec(context.Background(),
		`DELETE FROM discovery_events WHERE event_type = 'discography_observed'`)

	now := time.Now().UTC()
	counts := map[string]int{"spotify": 10, "musicbrainz": 2}

	// fresh + midwindow are inside every readable window; boundary sits exactly at
	// the retention cutoff (a strict `<` must keep it); ancient is past the window.
	seedObservation(t, store, now.Add(-1*time.Hour), "spotify:fresh", 10, 8, counts)
	seedObservation(t, store, now.AddDate(0, 0, -365), "spotify:midwindow", 10, 8, counts)
	seedObservation(t, store, now.Add(-discographyRetentionWindow), "spotify:boundary", 10, 8, counts)
	seedObservation(t, store, now.Add(-discographyRetentionWindow-24*time.Hour), "spotify:ancient", 10, 8, counts)

	pruned, err := store.PruneDiscographyObserved(context.Background(), now)
	if err != nil {
		t.Fatalf("PruneDiscographyObserved: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("pruned = %d, want 1 (only the row past the retention window)", pruned)
	}

	remaining := remainingDiscographyRefs(t, store)
	if remaining["spotify:ancient"] {
		t.Fatalf("ancient row survived the prune: %v", remaining)
	}
	for _, ref := range []string{"spotify:fresh", "spotify:midwindow", "spotify:boundary"} {
		if !remaining[ref] {
			t.Fatalf("in-window row %s was evicted (prune deleted in-window data): %v", ref, remaining)
		}
	}

	// The capped aggregate read at the widest window (365 days) must never surface
	// the evicted row — the scan stays within the retained window.
	since := now.AddDate(0, 0, -365)
	cases, err := store.DiscographyQuality(context.Background(), since, ports.GroupByArtist, 200)
	if err != nil {
		t.Fatalf("DiscographyQuality: %v", err)
	}
	for _, c := range cases {
		if c.ArtistRef == "spotify:ancient" {
			t.Fatalf("evicted row reached the aggregate read: %+v", c)
		}
	}
}

// TestPgxEventStore_PruneDiscographyObserved_Idempotent proves a second prune with
// no new rows removes nothing and cannot drop an in-window row: the guard against a
// prune that widens its reach on repeat, and against a clock edge deleting live data.
func TestPgxEventStore_PruneDiscographyObserved_Idempotent(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM discovery_events WHERE event_type = 'discography_observed'`)
	})
	_, _ = pool.Exec(context.Background(),
		`DELETE FROM discovery_events WHERE event_type = 'discography_observed'`)

	now := time.Now().UTC()
	seedObservation(t, store, now.Add(-1*time.Hour), "spotify:fresh", 10, 8, map[string]int{"spotify": 10})

	first, err := store.PruneDiscographyObserved(context.Background(), now)
	if err != nil {
		t.Fatalf("first prune: %v", err)
	}
	second, err := store.PruneDiscographyObserved(context.Background(), now)
	if err != nil {
		t.Fatalf("second prune: %v", err)
	}
	if first != 0 || second != 0 {
		t.Fatalf("prune removed in-window rows: first=%d second=%d, want 0 and 0", first, second)
	}
	if refs := remainingDiscographyRefs(t, store); !refs["spotify:fresh"] {
		t.Fatalf("in-window row lost across repeated prunes: %v", refs)
	}
}
