package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"testing"
	"time"
)

func seedObservation(t *testing.T, store *PgxEventStore, at time.Time, artistRef string, releases, single int, counts map[string]int) {
	t.Helper()
	if err := store.Append(context.Background(), domain.InteractionEvent{
		OccurredAt: at,
		UserId:     shared.SystemUserId(),
		Type:       domain.EventTypeDiscographyObserved,
		Payload: map[string]any{
			"artist_ref":      artistRef,
			"releases":        releases,
			"single_provider": single,
			"provider_counts": counts,
		},
	}); err != nil {
		t.Fatalf("append %s: %v", artistRef, err)
	}
}

func seedObservationWithNoID(t *testing.T, store *PgxEventStore, at time.Time, artistRef string, releases, single, noID int, counts map[string]int) {
	t.Helper()
	if err := store.Append(context.Background(), domain.InteractionEvent{
		OccurredAt: at,
		UserId:     shared.SystemUserId(),
		Type:       domain.EventTypeDiscographyObserved,
		Payload: map[string]any{
			"artist_ref":            artistRef,
			"releases":              releases,
			"single_provider":       single,
			"single_provider_no_id": noID,
			"provider_counts":       counts,
		},
	}); err != nil {
		t.Fatalf("append %s: %v", artistRef, err)
	}
}

func discographyRefs(cases []ports.DiscographyCase) []string {
	out := make([]string, len(cases))
	for i, c := range cases {
		out[i] = c.ArtistRef
	}
	return out
}

func TestPgxEventStore_DiscographyQuality_WorstFirstAndRegroup(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM discovery_events WHERE event_type = 'discography_observed'`)
	})
	_, _ = pool.Exec(context.Background(),
		`DELETE FROM discovery_events WHERE event_type = 'discography_observed'`)

	now := time.Now().UTC()
	seedObservation(t, store, now.Add(-1*time.Hour), "spotify:clean", 10, 0, map[string]int{"spotify": 10, "musicbrainz": 10})
	seedObservation(t, store, now.Add(-2*time.Hour), "spotify:worst", 10, 9, map[string]int{"spotify": 10, "musicbrainz": 1})
	seedObservation(t, store, now.Add(-48*time.Hour), "spotify:worst", 4, 0, map[string]int{"spotify": 4})
	seedObservation(t, store, now.Add(-3*time.Hour), "deezer:mid", 10, 5, map[string]int{"deezer": 8, "musicbrainz": 4})

	since := now.AddDate(0, 0, -30)

	byArtist, err := store.DiscographyQuality(context.Background(), since, ports.GroupByArtist, 200)
	if err != nil {
		t.Fatalf("DiscographyQuality(artist): %v", err)
	}
	if b, _ := json.Marshal(byArtist); true {
		t.Logf("by=artist -> %s", b)
	}
	if len(byArtist) != 3 {
		t.Fatalf("cases = %d, want 3 (latest-per-artist)", len(byArtist))
	}
	if byArtist[0].ArtistRef != "spotify:worst" || byArtist[0].Releases != 10 {
		t.Fatalf("worst-first head = %+v, want spotify:worst releases=10 (latest, not stale)", byArtist[0])
	}
	if got, want := discographyRefs(byArtist), []string{"spotify:worst", "deezer:mid", "spotify:clean"}; !sameOrder(got, want) {
		t.Fatalf("by=artist order = %v, want %v", got, want)
	}

	byProvider, err := store.DiscographyQuality(context.Background(), since, ports.GroupByProvider, 200)
	if err != nil {
		t.Fatalf("DiscographyQuality(provider): %v", err)
	}
	if b, _ := json.Marshal(byProvider); true {
		t.Logf("by=provider -> %s", b)
	}
	if got, want := discographyRefs(byProvider), []string{"spotify:worst", "deezer:mid", "spotify:clean"}; !sameOrder(got, want) {
		t.Fatalf("by=provider order = %v, want %v", got, want)
	}
}

func TestPgxEventStore_DiscographyQuality_IDAnchoredRanking(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM discovery_events WHERE event_type = 'discography_observed'`)
	})
	_, _ = pool.Exec(context.Background(),
		`DELETE FROM discovery_events WHERE event_type = 'discography_observed'`)

	now := time.Now().UTC()
	seedObservationWithNoID(t, store, now.Add(-1*time.Hour), "spotify:id-verified", 10, 10, 0, map[string]int{"spotify": 10})
	seedObservationWithNoID(t, store, now.Add(-2*time.Hour), "deezer:no-id", 10, 3, 3, map[string]int{"deezer": 7, "musicbrainz": 3})

	since := now.AddDate(0, 0, -30)
	cases, err := store.DiscographyQuality(context.Background(), since, ports.GroupByArtist, 200)
	if err != nil {
		t.Fatalf("DiscographyQuality: %v", err)
	}
	if b, _ := json.Marshal(cases); true {
		t.Logf("id-anchored order -> %s", b)
	}
	if got, want := discographyRefs(cases), []string{"deezer:no-id", "spotify:id-verified"}; !sameOrder(got, want) {
		t.Fatalf("id-anchored order = %v, want %v (no-id artist first; id-verified single-provider is not a top suspect)", got, want)
	}
	if cases[0].SingleProviderNoID != 3 {
		t.Fatalf("served single_provider_no_id = %d, want 3", cases[0].SingleProviderNoID)
	}
}

func clearDiscographyObserved(t *testing.T, store *PgxEventStore) {
	t.Helper()
	_, _ = store.pool.Exec(context.Background(),
		`DELETE FROM discovery_events WHERE event_type = 'discography_observed'`)
}

func TestPgxEventStore_SuspectRate_WindowedRealOpens(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	t.Cleanup(func() { clearDiscographyObserved(t, store) })
	clearDiscographyObserved(t, store)

	now := time.Now().UTC()
	newest := now.Add(-1 * time.Hour)
	seedObservationWithNoID(t, store, newest, "a", 10, 3, 3, map[string]int{"deezer": 7, "musicbrainz": 3})
	seedObservationWithNoID(t, store, now.Add(-2*time.Hour), "b", 8, 2, 1, map[string]int{"spotify": 6, "deezer": 2})
	seedObservationWithNoID(t, store, now.Add(-3*time.Hour), "c", 5, 5, 0, map[string]int{"spotify": 5})

	got, err := store.SuspectRate(context.Background(), now.AddDate(0, 0, -30))
	if err != nil {
		t.Fatalf("SuspectRate: %v", err)
	}
	if want := 2.0 / 3.0; got.Rate < want-1e-9 || got.Rate > want+1e-9 {
		t.Fatalf("rate = %v, want %v (2 of 3 opens fired the top suspect)", got.Rate, want)
	}
	if diff := got.LastSample.Sub(newest); diff < -time.Second || diff > time.Second {
		t.Fatalf("last sample = %v, want ~%v (the most recent open)", got.LastSample, newest)
	}
}

func clearSystemNonDiscographyEvents(t *testing.T, store *PgxEventStore) {
	t.Helper()
	_, _ = store.pool.Exec(context.Background(),
		`DELETE FROM discovery_events WHERE user_id = $1 AND event_type IN ('search_performed', 'play')`,
		shared.SystemUserId())
}

func TestPgxEventStore_SuspectRate_EvalRunLeavesNoSystemEvents(t *testing.T) {
	store := NewPgxEventStore(testPool(t))
	t.Run("eval run", TestPgxEventStore_SuspectRate_EvalRunDoesNotMoveIt)

	var left int
	if err := store.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM discovery_events WHERE user_id = $1 AND event_type IN ('search_performed', 'play')`,
		shared.SystemUserId()).Scan(&left); err != nil {
		t.Fatalf("count leftovers: %v", err)
	}
	if left != 0 {
		t.Fatalf("system search_performed/play rows left behind = %d, want 0", left)
	}
}

func TestPgxEventStore_SuspectRate_EvalRunDoesNotMoveIt(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	t.Cleanup(func() { clearDiscographyObserved(t, store) })
	t.Cleanup(func() { clearSystemNonDiscographyEvents(t, store) })
	clearDiscographyObserved(t, store)

	now := time.Now().UTC()
	since := now.AddDate(0, 0, -30)
	seedObservationWithNoID(t, store, now.Add(-1*time.Hour), "suspect", 4, 4, 4, map[string]int{"deezer": 4})
	seedObservationWithNoID(t, store, now.Add(-2*time.Hour), "clean", 4, 0, 0, map[string]int{"spotify": 2, "deezer": 2})

	before, err := store.SuspectRate(context.Background(), since)
	if err != nil {
		t.Fatalf("SuspectRate before: %v", err)
	}

	for i := 0; i < 20; i++ {
		at := now.Add(-time.Duration(i) * time.Minute)
		seedNonDiscographyEvent(t, store, at, domain.EventTypeSearchPerformed)
		seedNonDiscographyEvent(t, store, at, domain.EventTypePlay)
	}

	after, err := store.SuspectRate(context.Background(), since)
	if err != nil {
		t.Fatalf("SuspectRate after: %v", err)
	}
	if after.Rate != before.Rate {
		t.Fatalf("an eval run moved the suspect rate: before=%v after=%v (rate must count real opens only)",
			before.Rate, after.Rate)
	}
}

func TestPgxEventStore_SuspectRate_EmptyWindowIsZero(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	t.Cleanup(func() { clearDiscographyObserved(t, store) })
	clearDiscographyObserved(t, store)

	got, err := store.SuspectRate(context.Background(), time.Now().UTC().AddDate(0, 0, -30))
	if err != nil {
		t.Fatalf("SuspectRate: %v", err)
	}
	if got.Rate != 0 {
		t.Fatalf("rate = %v, want 0 for an empty window", got.Rate)
	}
	if !got.LastSample.IsZero() {
		t.Fatalf("last sample = %v, want zero for an empty window", got.LastSample)
	}
}

func seedNonDiscographyEvent(t *testing.T, store *PgxEventStore, at time.Time, eventType domain.EventType) {
	t.Helper()
	if err := store.Append(context.Background(), domain.InteractionEvent{
		OccurredAt: at,
		UserId:     shared.SystemUserId(),
		Type:       eventType,
		Payload:    map[string]any{"result_signature": "synthetic"},
	}); err != nil {
		t.Fatalf("append %s: %v", eventType, err)
	}
}

func sameOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

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
