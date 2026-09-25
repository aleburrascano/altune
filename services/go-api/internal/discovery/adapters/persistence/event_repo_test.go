package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestPgxEventStore_AppendAndRead(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM discovery_events WHERE user_id = $1`, userId.UUID())
	})

	event := domain.InteractionEvent{
		UserId:    userId,
		Type:      domain.EventTypeSearchPerformed,
		QueryNorm: "kendrick lamar",
		Payload: map[string]any{
			"result_count": 12,
			"zero_result":  false,
		},
	}

	if err := store.Append(ctx, event); err != nil {
		t.Fatalf("Append: %v", err)
	}

	var (
		eventType string
		queryNorm *string
		payload   []byte
	)
	err := pool.QueryRow(ctx,
		`SELECT event_type, query_norm, payload FROM discovery_events WHERE user_id = $1`,
		userId.UUID(),
	).Scan(&eventType, &queryNorm, &payload)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if eventType != "search_performed" {
		t.Errorf("event_type = %q, want search_performed", eventType)
	}
	if queryNorm == nil || *queryNorm != "kendrick lamar" {
		t.Errorf("query_norm = %v, want kendrick lamar", queryNorm)
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if rc, ok := got["result_count"].(float64); !ok || rc != 12 {
		t.Errorf("payload result_count = %v, want 12", got["result_count"])
	}
}

func TestPgxEventStore_NilPayloadAndQuery(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM discovery_events WHERE user_id = $1`, userId.UUID())
	})

	event := domain.InteractionEvent{
		UserId: userId,
		Type:   domain.EventTypeWrongAlbum,
	}

	if err := store.Append(ctx, event); err != nil {
		t.Fatalf("Append: %v", err)
	}

	var (
		eventType string
		queryNorm *string
		payload   []byte
	)
	err := pool.QueryRow(ctx,
		`SELECT event_type, query_norm, payload FROM discovery_events WHERE user_id = $1`,
		userId.UUID(),
	).Scan(&eventType, &queryNorm, &payload)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if eventType != "wrong_album" {
		t.Errorf("event_type = %q, want wrong_album", eventType)
	}
	if queryNorm != nil {
		t.Errorf("query_norm = %v, want NULL", *queryNorm)
	}
	if string(payload) != "{}" {
		t.Errorf("payload = %q, want {}", string(payload))
	}
}

// dc builds a DiscographyCase; providerCounts pairs are name,count,name,count...
func dc(ref string, releases, single int, pc ...any) ports.DiscographyCase {
	counts := map[string]int{}
	for i := 0; i+1 < len(pc); i += 2 {
		counts[pc[i].(string)] = pc[i+1].(int)
	}
	return ports.DiscographyCase{ArtistRef: ref, Releases: releases, SingleProvider: single, ProviderCounts: counts}
}

// dcNoID builds a DiscographyCase with an explicit no-id suspect count (the
// single-provider releases that also lack a shared id — the real suspects the
// worst-first order ranks on).
func dcNoID(ref string, releases, single, noID int, pc ...any) ports.DiscographyCase {
	c := dc(ref, releases, single, pc...)
	c.SingleProviderNoID = noID
	return c
}

func refs(cases []ports.DiscographyCase) []string {
	out := make([]string, len(cases))
	for i, c := range cases {
		out[i] = c.ArtistRef
	}
	return out
}

func eq(a, b []string) bool {
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

// TestRankByArtist_WorstFirst is the core seam guarantee: the artist with the
// highest single-provider ratio ranks first, imbalance breaks a ratio tie.
func TestRankByArtist_WorstFirst(t *testing.T) {
	cases := []ports.DiscographyCase{
		dc("clean", 10, 0, "spotify", 10, "musicbrainz", 10),    // ratio 0.0
		dc("worst", 10, 9, "spotify", 10, "musicbrainz", 1),     // ratio 0.9
		dc("mid", 10, 5, "spotify", 8, "musicbrainz", 4),        // ratio 0.5
		dc("tie-lo-imb", 10, 9, "spotify", 6, "musicbrainz", 5), // ratio 0.9, imbalance 1
	}
	got := refs(rankDiscographyCases(cases, ports.GroupByArtist))
	want := []string{"worst", "tie-lo-imb", "mid", "clean"}
	if !eq(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestRankByArtist_IDAnchorBeatsHeadcount plants the id-anchor core rule: the
// worst-first order ranks by the no-id suspect ratio, not raw provider headcount.
// An artist whose single-provider releases are all id-verified is NOT top-ranked
// even though its plain headcount ratio is the highest; a no-id single-provider
// artist with a lower headcount ratio IS. The id, not the headcount, is the anchor.
func TestRankByArtist_IDAnchorBeatsHeadcount(t *testing.T) {
	cases := []ports.DiscographyCase{
		// Every single-provider release carries a shared id: headcount ratio 1.0
		// (the old top suspect), but zero real suspects.
		dcNoID("id-verified", 10, 10, 0, "spotify", 10),
		// Fewer single-provider releases and a lower headcount ratio, but none carry
		// an id: these are the real suspects.
		dcNoID("no-id", 10, 3, 3, "spotify", 7, "deezer", 3),
	}
	got := refs(rankDiscographyCases(cases, ports.GroupByArtist))
	want := []string{"no-id", "id-verified"}
	if !eq(got, want) {
		t.Fatalf("order = %v, want %v (id anchor ranks the no-id artist first; headcount is only the fallback)", got, want)
	}
	if got[len(got)-1] != "id-verified" {
		t.Fatalf("id-verified single-provider artist ranked %v, want last (not a top suspect)", got)
	}
}

// TestRankByArtist_NoCrownedSource plants the no-crowned-source core rule: a no-id
// single-provider release supplied only by a reputable id source (musicbrainz)
// ranks as suspect exactly like one from any other provider. The primary score is
// invariant under provider identity, so no provider name short-circuits it to
// "not a suspect".
func TestRankByArtist_NoCrownedSource(t *testing.T) {
	reputable := dcNoID("z-only-musicbrainz", 4, 4, 4, "musicbrainz", 4)
	other := dcNoID("a-only-genius", 4, 4, 4, "genius", 4)
	if noIDSuspectRatio(reputable) != noIDSuspectRatio(other) {
		t.Fatalf("a provider name changed the suspect ratio (musicbrainz=%v genius=%v): a source was crowned truth",
			noIDSuspectRatio(reputable), noIDSuspectRatio(other))
	}
	// Identical suspect ratios: the only remaining tie-break is artist_ref, never
	// the provider — so the alphabetically-first ref leads, not the "trusted" source.
	got := refs(rankDiscographyCases([]ports.DiscographyCase{reputable, other}, ports.GroupByArtist))
	if want := []string{"a-only-genius", "z-only-musicbrainz"}; !eq(got, want) {
		t.Fatalf("order = %v, want %v (tie broken by artist_ref, not by crowning a provider)", got, want)
	}
}

// TestRankByArtist_NoStaticTrustWeights plants the no-hand-set-weights core rule:
// the suspect score is a function of the id fact and the release counts only. Two
// cases with the same no-id/releases counts but different provider mixes and
// magnitudes score identically, so no static per-provider trust weight enters.
func TestRankByArtist_NoStaticTrustWeights(t *testing.T) {
	a := dcNoID("a", 10, 5, 5, "spotify", 100, "deezer", 1)
	b := dcNoID("b", 10, 5, 5, "musicbrainz", 2, "genius", 50)
	if noIDSuspectRatio(a) != noIDSuspectRatio(b) {
		t.Fatalf("provider mix changed the suspect score (a=%v b=%v): a static per-provider weight leaked in",
			noIDSuspectRatio(a), noIDSuspectRatio(b))
	}
}

// TestRankByArtist_DivideByZeroSafe proves a zero-release (or negative) case never
// panics and never sorts as worst: its ratio is 0.
func TestRankByArtist_DivideByZeroSafe(t *testing.T) {
	cases := []ports.DiscographyCase{
		dc("zero-releases", 0, 5),
		dc("negative", -3, -9),
		dc("real", 4, 3, "spotify", 4, "deezer", 1),
	}
	got := refs(rankDiscographyCases(cases, ports.GroupByArtist))
	if got[0] != "real" {
		t.Fatalf("worst-first = %v, want real first (zero/neg ratios are 0)", got)
	}
}

// TestRankByProvider_ClustersWorstFirst proves by=provider clusters artists by
// their dominant provider and orders the clusters worst-first by aggregate ratio.
func TestRankByProvider_ClustersWorstFirst(t *testing.T) {
	cases := []ports.DiscographyCase{
		// deezer-dominant cluster, low contamination
		dc("d1", 10, 1, "deezer", 8, "spotify", 3),
		// spotify-dominant cluster, high contamination
		dc("s1", 10, 9, "spotify", 10, "deezer", 1),
		dc("s2", 10, 7, "spotify", 9, "deezer", 2),
	}
	got := refs(rankDiscographyCases(cases, ports.GroupByProvider))
	// spotify cluster (agg ratio 0.8) before deezer cluster (0.1); within spotify
	// s1 (0.9) before s2 (0.7).
	want := []string{"s1", "s2", "d1"}
	if !eq(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestRankByContaminationBand orders high band before medium before low, worst
// artist first within a band.
func TestRankByContaminationBand(t *testing.T) {
	cases := []ports.DiscographyCase{
		dc("low", 10, 1),            // 0.1 -> low
		dc("high-a", 10, 6, "s", 6), // 0.6 -> high
		dc("medium", 10, 3),         // 0.3 -> medium
		dc("high-b", 10, 9, "s", 9), // 0.9 -> high
	}
	got := refs(rankDiscographyCases(cases, ports.GroupByContaminationBand))
	want := []string{"high-b", "high-a", "medium", "low"}
	if !eq(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestRank_UnknownGroupByIsArtist proves an unknown grouping degrades to the
// artist ordering rather than dropping or reordering unpredictably.
func TestRank_UnknownGroupByIsArtist(t *testing.T) {
	cases := []ports.DiscographyCase{
		dc("a", 10, 2),
		dc("b", 10, 8),
	}
	got := refs(rankDiscographyCases(cases, ports.DiscographyGroupBy("bogus")))
	if want := []string{"b", "a"}; !eq(got, want) {
		t.Fatalf("order = %v, want %v (artist default)", got, want)
	}
}

// TestProviderImbalance_AdversarialCounts proves negative provider counts are
// floored so imbalance stays non-negative and a lone provider has no imbalance.
func TestProviderImbalance_AdversarialCounts(t *testing.T) {
	if got := providerImbalance(dc("x", 5, 1, "only", 5)); got != 0 {
		t.Errorf("lone provider imbalance = %d, want 0", got)
	}
	if got := providerImbalance(dc("x", 5, 1, "a", -4, "b", 3)); got != 3 {
		t.Errorf("adversarial imbalance = %d, want 3 (negatives floored)", got)
	}
}
