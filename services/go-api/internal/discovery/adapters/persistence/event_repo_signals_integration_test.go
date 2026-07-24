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

// appendOrFatal appends one event and fails the test on error.
func appendOrFatal(t *testing.T, store *PgxEventStore, ev domain.InteractionEvent) {
	t.Helper()
	if err := store.Append(context.Background(), ev); err != nil {
		t.Fatalf("Append(%s): %v", ev.Type.String(), err)
	}
}

func newEventTestUser(t *testing.T, store *PgxEventStore) shared.UserId {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(),
			`DELETE FROM discovery_events WHERE user_id = $1`, userId.UUID())
	})
	return userId
}

// SatisfactionSignals: net score per result_signature with the per-user ±3 cap,
// short-dwell semantics, and the jsonb_typeof guard against poisoned payloads.
func TestPgxEventStore_SatisfactionSignals(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	ctx := context.Background()

	userA := newEventTestUser(t, store)
	userB := newEventTestUser(t, store)
	userC := newEventTestUser(t, store)

	suffix := uuid.New().String()[:8]
	sigCapped := "sig-cap-" + suffix     // one user, 10 plays → capped at +3
	sigMixed := "sig-mixed-" + suffix    // +2 (play+completed) − 1 (short skip) = +1
	sigPoisoned := "sig-poison-" + suffix // 1 play, poisoned/unknown-dwell skips count 0 → +1
	sigZeroNet := "sig-zeronet-" + suffix // +1 −1 = 0 → excluded by HAVING

	// Per-user cap: 10 plays from ONE user must contribute at most +3.
	for range 10 {
		appendOrFatal(t, store, domain.InteractionEvent{
			UserId: userA, Type: domain.EventTypePlay,
			Payload: map[string]any{"result_signature": sigCapped},
		})
	}

	// Mixed: play + completed are each +1; a short-dwell skip is −1.
	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userA, Type: domain.EventTypePlay,
		Payload: map[string]any{"result_signature": sigMixed},
	})
	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userA, Type: domain.EventTypeCompleted,
		Payload: map[string]any{"result_signature": sigMixed},
	})
	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userB, Type: domain.EventTypeSkip,
		Payload: map[string]any{"result_signature": sigMixed, "dwell_ms": 1500},
	})

	// Poisoned payloads: dwell_ms as string, as object, absent, and ≥ threshold.
	// Every one of these skips must count 0 — and, critically, must not 22P02
	// the whole query. The single play nets the signature to exactly +1.
	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userB, Type: domain.EventTypePlay,
		Payload: map[string]any{"result_signature": sigPoisoned},
	})
	for _, dwell := range []any{"abc", map[string]any{"ms": 5}, nil, 25000} {
		payload := map[string]any{"result_signature": sigPoisoned}
		if dwell != nil {
			payload["dwell_ms"] = dwell
		}
		appendOrFatal(t, store, domain.InteractionEvent{
			UserId: userB, Type: domain.EventTypeSkip, Payload: payload,
		})
	}

	// Net zero: one play, one short skip — HAVING <> 0 must drop it.
	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userC, Type: domain.EventTypePlay,
		Payload: map[string]any{"result_signature": sigZeroNet},
	})
	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userC, Type: domain.EventTypeSkip,
		Payload: map[string]any{"result_signature": sigZeroNet, "dwell_ms": 100},
	})

	signals, err := store.SatisfactionSignals(ctx, time.Now().UTC().Add(-time.Minute))
	if err != nil {
		t.Fatalf("SatisfactionSignals: %v (poisoned payloads must not error the window)", err)
	}
	scores := map[string]float64{}
	for _, s := range signals {
		scores[s.ResultSignature] = s.Score
	}

	if got := scores[sigCapped]; got != 3 {
		t.Errorf("capped signature score = %v, want 3 (10 plays from one user capped at +3)", got)
	}
	if got := scores[sigMixed]; got != 1 {
		t.Errorf("mixed signature score = %v, want 1 (+1 play +1 completed −1 short skip)", got)
	}
	if got := scores[sigPoisoned]; got != 1 {
		t.Errorf("poisoned signature score = %v, want 1 (string/object/absent/long dwell skips all count 0)", got)
	}
	if _, present := scores[sigZeroNet]; present {
		t.Errorf("net-zero signature present with score %v, want excluded (HAVING <> 0)", scores[sigZeroNet])
	}
}

// ZeroResultQueries: only boolean-true zero_result rows count; poisoned
// (string) and absent flags are skipped without erroring.
func TestPgxEventStore_ZeroResultQueries(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	ctx := context.Background()
	userId := newEventTestUser(t, store)

	suffix := uuid.New().String()[:8]
	qHit := "qa zero hit " + suffix
	qPoison := "qa zero poison " + suffix
	qAbsent := "qa zero absent " + suffix
	qFalse := "qa zero false " + suffix

	search := func(queryNorm string, payload map[string]any) {
		appendOrFatal(t, store, domain.InteractionEvent{
			UserId: userId, Type: domain.EventTypeSearchPerformed,
			QueryNorm: queryNorm, Payload: payload,
		})
	}
	search(qHit, map[string]any{"zero_result": true})
	search(qHit, map[string]any{"zero_result": true})
	search(qPoison, map[string]any{"zero_result": "yes"}) // poisoned: string, not boolean
	search(qAbsent, map[string]any{})
	search(qFalse, map[string]any{"zero_result": false})

	counts, err := store.ZeroResultQueries(ctx, time.Now().UTC().Add(-time.Minute), 1000)
	if err != nil {
		t.Fatalf("ZeroResultQueries: %v (poisoned zero_result must not error)", err)
	}
	got := map[string]int{}
	for _, qc := range counts {
		got[qc.QueryNorm] = qc.Count
	}
	if got[qHit] != 2 {
		t.Errorf("count(%q) = %d, want 2", qHit, got[qHit])
	}
	for _, q := range []string{qPoison, qAbsent, qFalse} {
		if _, present := got[q]; present {
			t.Errorf("%q counted as zero-result, want excluded", q)
		}
	}
}

// NonZeroNoClickQueries: searches that returned results but drew no click.
func TestPgxEventStore_NonZeroNoClickQueries(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	ctx := context.Background()
	userId := newEventTestUser(t, store)

	suffix := uuid.New().String()[:8]
	qNoClick := "qa noclick " + suffix
	qClicked := "qa clicked " + suffix
	qZero := "qa noclick zero " + suffix
	qPoison := "qa noclick poison " + suffix

	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.EventTypeSearchPerformed,
		QueryNorm: qNoClick, Payload: map[string]any{"zero_result": false},
	})
	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.EventTypeSearchPerformed,
		QueryNorm: qClicked, Payload: map[string]any{"zero_result": false},
	})
	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.EventTypeResultClicked,
		QueryNorm: qClicked, Payload: map[string]any{"result_signature": "s"},
	})
	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.EventTypeSearchPerformed,
		QueryNorm: qZero, Payload: map[string]any{"zero_result": true},
	})
	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.EventTypeSearchPerformed,
		QueryNorm: qPoison, Payload: map[string]any{"zero_result": "nope"},
	})

	counts, err := store.NonZeroNoClickQueries(ctx, time.Now().UTC().Add(-time.Minute), 1000)
	if err != nil {
		t.Fatalf("NonZeroNoClickQueries: %v (poisoned zero_result must not error)", err)
	}
	got := map[string]int{}
	for _, qc := range counts {
		got[qc.QueryNorm] = qc.Count
	}
	if got[qNoClick] != 1 {
		t.Errorf("count(%q) = %d, want 1 (non-zero search, no click)", qNoClick, got[qNoClick])
	}
	for _, q := range []string{qClicked, qZero, qPoison} {
		if _, present := got[q]; present {
			t.Errorf("%q counted, want excluded (clicked / zero-result / poisoned)", q)
		}
	}
}

// BehavioralLabels: engagement chained to its search by search_id; wrong_album
// is a hard negative that trumps a positive on the same signature.
func TestPgxEventStore_BehavioralLabels(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	ctx := context.Background()
	userId := newEventTestUser(t, store)

	suffix := uuid.New().String()[:8]
	qPos := "qa label pos " + suffix
	qNeg := "qa label neg " + suffix
	searchPos := uuid.New().String()
	searchNeg := uuid.New().String()
	sigPos := "sig-lbl-pos-" + suffix
	sigNeg := "sig-lbl-neg-" + suffix

	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.EventTypeSearchPerformed,
		QueryNorm: qPos, SearchId: searchPos,
	})
	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.EventTypeCompleted, SearchId: searchPos,
		Payload: map[string]any{
			"result_signature": sigPos, "title": "Track Title", "subtitle": "Artist Name",
		},
	})

	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.EventTypeSearchPerformed,
		QueryNorm: qNeg, SearchId: searchNeg,
	})
	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.EventTypeLibraryAdd, SearchId: searchNeg,
		Payload: map[string]any{"result_signature": sigNeg, "title": "Wrong One"},
	})
	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.EventTypeWrongAlbum, SearchId: searchNeg,
		Payload: map[string]any{"result_signature": sigNeg, "title": "Wrong One"},
	})

	// An engagement with NO search_id can't chain to a query — no label.
	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.EventTypeCompleted,
		Payload: map[string]any{"result_signature": "sig-lbl-orphan-" + suffix},
	})

	labels, err := store.BehavioralLabels(ctx, time.Now().UTC().Add(-time.Minute))
	if err != nil {
		t.Fatalf("BehavioralLabels: %v", err)
	}
	bySig := map[string]struct {
		query    string
		polarity int
		title    string
		subtitle string
	}{}
	for _, l := range labels {
		bySig[l.ResultSignature] = struct {
			query    string
			polarity int
			title    string
			subtitle string
		}{l.QueryNorm, l.Polarity, l.Title, l.Subtitle}
	}

	pos, ok := bySig[sigPos]
	if !ok {
		t.Fatalf("no label mined for completed chain %q", sigPos)
	}
	if pos.query != qPos || pos.polarity != 1 {
		t.Errorf("positive label = (%q, %+d), want (%q, +1)", pos.query, pos.polarity, qPos)
	}
	if pos.title != "Track Title" || pos.subtitle != "Artist Name" {
		t.Errorf("label carried (%q, %q), want payload title/subtitle", pos.title, pos.subtitle)
	}

	neg, ok := bySig[sigNeg]
	if !ok {
		t.Fatalf("no label mined for wrong_album chain %q", sigNeg)
	}
	if neg.polarity != -1 {
		t.Errorf("wrong_album polarity = %+d, want -1 (hard negative trumps the library_add)", neg.polarity)
	}

	if _, present := bySig["sig-lbl-orphan-"+suffix]; present {
		t.Error("engagement without search_id produced a label, want none")
	}
}

// AbandonedSearches: no click + a same-session reformulation within 60s.
// The 59s/61s pair pins the window boundary.
func TestPgxEventStore_AbandonedSearches(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	ctx := context.Background()
	userId := newEventTestUser(t, store)

	suffix := uuid.New().String()[:8]
	base := time.Now().UTC().Add(-10 * time.Minute)

	search := func(queryNorm, sessionID, searchID string, at time.Time) {
		appendOrFatal(t, store, domain.InteractionEvent{
			UserId: userId, Type: domain.EventTypeSearchPerformed,
			QueryNorm: queryNorm, SearchId: searchID, OccurredAt: at,
			Payload: map[string]any{"session_id": sessionID},
		})
	}

	// Reformulated 30s later, never clicked → abandoned.
	qAband := "qa aband " + suffix
	search(qAband, "sess-a-"+suffix, "", base)
	search("qa aband next "+suffix, "sess-a-"+suffix, "", base.Add(30*time.Second))

	// Boundary: reformulated at +59s → inside the window, abandoned.
	q59 := "qa aband 59s " + suffix
	search(q59, "sess-b-"+suffix, "", base)
	search("qa aband 59s next "+suffix, "sess-b-"+suffix, "", base.Add(59*time.Second))

	// Boundary: reformulated at +61s → outside the window, NOT abandoned.
	q61 := "qa aband 61s " + suffix
	search(q61, "sess-c-"+suffix, "", base)
	search("qa aband 61s next "+suffix, "sess-c-"+suffix, "", base.Add(61*time.Second))

	// Clicked, then reformulated → NOT abandoned (the click satisfies it).
	qClicked := "qa aband clicked " + suffix
	clickedSearchID := uuid.New().String()
	search(qClicked, "sess-d-"+suffix, clickedSearchID, base)
	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.EventTypeResultClicked, SearchId: clickedSearchID,
		OccurredAt: base.Add(5 * time.Second),
		Payload:    map[string]any{"result_signature": "s"},
	})
	search("qa aband clicked next "+suffix, "sess-d-"+suffix, "", base.Add(30*time.Second))

	counts, err := store.AbandonedSearches(ctx, base.Add(-time.Minute), 1000)
	if err != nil {
		t.Fatalf("AbandonedSearches: %v", err)
	}
	got := map[string]int{}
	for _, qc := range counts {
		got[qc.QueryNorm] = qc.Count
	}

	if got[qAband] != 1 {
		t.Errorf("count(%q) = %d, want 1 (no click + 30s reformulation)", qAband, got[qAband])
	}
	if got[q59] != 1 {
		t.Errorf("count(%q) = %d, want 1 (59s is inside the 60s window)", q59, got[q59])
	}
	if _, present := got[q61]; present {
		t.Errorf("%q counted as abandoned, want excluded (61s is outside the 60s window)", q61)
	}
	if _, present := got[qClicked]; present {
		t.Errorf("%q counted as abandoned, want excluded (it was clicked)", qClicked)
	}
}

// Append idempotency: a valid event_id dedups a retry; a malformed event_id is
// dropped (row still lands, WITHOUT dedup) and a malformed search_id persists
// as NULL rather than failing the append.
func TestPgxEventStore_Append_EventIDDedupAndMalformedIDs(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	ctx := context.Background()

	countRows := func(t *testing.T, userId shared.UserId) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM discovery_events WHERE user_id = $1`, userId.UUID(),
		).Scan(&n); err != nil {
			t.Fatalf("count rows: %v", err)
		}
		return n
	}

	t.Run("valid event_id dedups the retry", func(t *testing.T) {
		userId := newEventTestUser(t, store)
		eventID := uuid.New().String()
		ev := domain.InteractionEvent{
			UserId: userId, Type: domain.EventTypeLibraryAdd, EventId: eventID,
			Payload: map[string]any{"result_signature": "sig-dedup"},
		}
		appendOrFatal(t, store, ev)
		appendOrFatal(t, store, ev) // at-least-once retry
		if n := countRows(t, userId); n != 1 {
			t.Errorf("rows after retried critical event = %d, want 1 (ON CONFLICT no-op)", n)
		}
	})

	t.Run("malformed event_id inserts without dedup", func(t *testing.T) {
		userId := newEventTestUser(t, store)
		ev := domain.InteractionEvent{
			UserId: userId, Type: domain.EventTypeLibraryAdd, EventId: "not-a-uuid",
		}
		appendOrFatal(t, store, ev)
		appendOrFatal(t, store, ev)
		if n := countRows(t, userId); n != 2 {
			t.Errorf("rows = %d, want 2 (unparseable event_id → NULL, no dedup)", n)
		}
		var nullIDs int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM discovery_events WHERE user_id = $1 AND event_id IS NULL`,
			userId.UUID(),
		).Scan(&nullIDs); err != nil {
			t.Fatalf("count null event_ids: %v", err)
		}
		if nullIDs != 2 {
			t.Errorf("NULL event_id rows = %d, want 2", nullIDs)
		}
	})

	t.Run("malformed search_id persists as NULL", func(t *testing.T) {
		userId := newEventTestUser(t, store)
		appendOrFatal(t, store, domain.InteractionEvent{
			UserId: userId, Type: domain.EventTypePlay, SearchId: "bogus-not-a-uuid",
		})
		var searchID *uuid.UUID
		if err := pool.QueryRow(ctx,
			`SELECT search_id FROM discovery_events WHERE user_id = $1`, userId.UUID(),
		).Scan(&searchID); err != nil {
			t.Fatalf("read back: %v", err)
		}
		if searchID != nil {
			t.Errorf("search_id = %v, want NULL (malformed id dropped, event kept)", searchID)
		}
	})
}
