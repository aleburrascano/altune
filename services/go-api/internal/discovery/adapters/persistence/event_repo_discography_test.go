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

// seedObservation appends one discography_observed row through the real Append.
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
			"last_seen":       at.Format(time.RFC3339),
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

// TestPgxEventStore_DiscographyQuality_WorstFirstAndRegroup exercises the real SQL
// aggregate against Postgres: it seeds several artists (including a stale earlier
// observation that must be superseded by the latest) and proves the read reduces
// to one case per artist, ranks worst-first by contamination ratio, and regroups
// by dominant provider under by=provider. It logs the served cases for inspection.
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
	// A stale earlier observation of worst that the latest must supersede.
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
	// dominant providers: worst->spotify, mid->deezer, clean->musicbrainz (10/10
	// tie, lexicographically smallest). Cluster ratios spotify 0.9 > deezer 0.5 >
	// musicbrainz 0.0, so the regrouped order is worst, mid, clean.
	if got, want := discographyRefs(byProvider), []string{"spotify:worst", "deezer:mid", "spotify:clean"}; !sameOrder(got, want) {
		t.Fatalf("by=provider order = %v, want %v", got, want)
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
