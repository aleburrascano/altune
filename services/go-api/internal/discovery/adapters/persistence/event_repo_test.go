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

func dc(ref string, releases, single int, pc ...any) ports.DiscographyCase {
	counts := map[string]int{}
	for i := 0; i+1 < len(pc); i += 2 {
		counts[pc[i].(string)] = pc[i+1].(int)
	}
	return ports.DiscographyCase{ArtistRef: ref, Releases: releases, SingleProvider: single, ProviderCounts: counts}
}

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

func TestRankByArtist_WorstFirst(t *testing.T) {
	cases := []ports.DiscographyCase{
		dc("clean", 10, 0, "spotify", 10, "musicbrainz", 10),
		dc("worst", 10, 9, "spotify", 10, "musicbrainz", 1),
		dc("mid", 10, 5, "spotify", 8, "musicbrainz", 4),
		dc("tie-lo-imb", 10, 9, "spotify", 6, "musicbrainz", 5),
	}
	got := refs(rankDiscographyCases(cases, ports.GroupByArtist))
	want := []string{"worst", "tie-lo-imb", "mid", "clean"}
	if !eq(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestRankByArtist_IDAnchorBeatsHeadcount(t *testing.T) {
	cases := []ports.DiscographyCase{
		dcNoID("id-verified", 10, 10, 0, "spotify", 10),
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

func TestRankByArtist_NoCrownedSource(t *testing.T) {
	reputable := dcNoID("z-only-musicbrainz", 4, 4, 4, "musicbrainz", 4)
	other := dcNoID("a-only-genius", 4, 4, 4, "genius", 4)
	if noIDSuspectRatio(reputable) != noIDSuspectRatio(other) {
		t.Fatalf("a provider name changed the suspect ratio (musicbrainz=%v genius=%v): a source was crowned truth",
			noIDSuspectRatio(reputable), noIDSuspectRatio(other))
	}
	got := refs(rankDiscographyCases([]ports.DiscographyCase{reputable, other}, ports.GroupByArtist))
	if want := []string{"a-only-genius", "z-only-musicbrainz"}; !eq(got, want) {
		t.Fatalf("order = %v, want %v (tie broken by artist_ref, not by crowning a provider)", got, want)
	}
}

func TestRankByArtist_NoStaticTrustWeights(t *testing.T) {
	a := dcNoID("a", 10, 5, 5, "spotify", 100, "deezer", 1)
	b := dcNoID("b", 10, 5, 5, "musicbrainz", 2, "genius", 50)
	if noIDSuspectRatio(a) != noIDSuspectRatio(b) {
		t.Fatalf("provider mix changed the suspect score (a=%v b=%v): a static per-provider weight leaked in",
			noIDSuspectRatio(a), noIDSuspectRatio(b))
	}
}

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

func TestRankByProvider_ClustersWorstFirst(t *testing.T) {
	cases := []ports.DiscographyCase{
		dc("d1", 10, 1, "deezer", 8, "spotify", 3),
		dc("s1", 10, 9, "spotify", 10, "deezer", 1),
		dc("s2", 10, 7, "spotify", 9, "deezer", 2),
	}
	got := refs(rankDiscographyCases(cases, ports.GroupByProvider))
	want := []string{"s1", "s2", "d1"}
	if !eq(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestRankByContaminationBand(t *testing.T) {
	cases := []ports.DiscographyCase{
		dc("low", 10, 1),
		dc("high-a", 10, 6, "s", 6),
		dc("medium", 10, 3),
		dc("high-b", 10, 9, "s", 9),
	}
	got := refs(rankDiscographyCases(cases, ports.GroupByContaminationBand))
	want := []string{"high-b", "high-a", "medium", "low"}
	if !eq(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

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

func TestProviderImbalance_AdversarialCounts(t *testing.T) {
	if got := providerImbalance(dc("x", 5, 1, "only", 5)); got != 0 {
		t.Errorf("lone provider imbalance = %d, want 0", got)
	}
	if got := providerImbalance(dc("x", 5, 1, "a", -4, "b", 3)); got != 3 {
		t.Errorf("adversarial imbalance = %d, want 3 (negatives floored)", got)
	}
}
