package persistence_test

import (
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/ids"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func performSearch(t *testing.T, store *persistence.PostgresEventStore, userId ids.UserId, queryNorm, searchID string) {
	t.Helper()
	appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.InteractionTypeSearchPerformed,
		QueryNorm: queryNorm, SearchId: searchID,
		Payload: map[string]any{"zero_result": false},
	})
}

func storedQueryNorm(t *testing.T, pool *pgxpool.Pool, userId ids.UserId, eventID string) *string {
	t.Helper()
	var queryNorm *string
	if err := pool.QueryRow(context.Background(),
		`SELECT query_norm FROM discovery_events WHERE user_id = $1 AND event_id = $2`,
		userId.UUID(), uuid.MustParse(eventID),
	).Scan(&queryNorm); err != nil {
		t.Fatalf("read stored query_norm: %v", err)
	}
	return queryNorm
}

func TestPostgresEventStore_NonZeroNoClickQueries_IgnoresClientQueryNorm(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	store := persistence.NewPostgresEventStore(pool, clock.Now)
	ctx := context.Background()
	honest := newEventTestUser(t.Context(), t, pool)
	attacker := newEventTestUser(t.Context(), t, pool)

	suffix := uuid.New().String()[:8]
	qAnswered := "qn answered " + suffix
	qTarget := "qn target gap " + suffix
	answeredSearch := uuid.New().String()

	performSearch(t, store, honest, qAnswered, answeredSearch)
	appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
		UserId: honest, Type: domain.InteractionTypeResultClicked,
		SearchId: answeredSearch, QueryNorm: "garbled " + suffix,
		Payload: map[string]any{"result_signature": "s"},
	})

	performSearch(t, store, honest, qTarget, uuid.New().String())
	attackerSearch := uuid.New().String()
	performSearch(t, store, attacker, "qn attacker own "+suffix, attackerSearch)
	for _, searchID := range []string{"", attackerSearch} {
		appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
			UserId: attacker, Type: domain.InteractionTypeResultClicked,
			SearchId: searchID, QueryNorm: qTarget,
			Payload: map[string]any{"result_signature": "s"},
		})
	}

	counts, err := store.NonZeroNoClickQueries(ctx, time.Now().UTC().Add(-time.Minute), 1000)
	if err != nil {
		t.Fatalf("NonZeroNoClickQueries: %v", err)
	}
	got := map[string]int{}
	for _, qc := range counts {
		got[qc.QueryNorm] = qc.Count
	}
	if _, present := got[qAnswered]; present {
		t.Errorf("%q reported as no-click, want excluded (its search was clicked under a garbled client query_norm)", qAnswered)
	}
	if got[qTarget] != 1 {
		t.Errorf("count(%q) = %d, want 1 (a forged client query_norm must not mark it answered)", qTarget, got[qTarget])
	}
}

func TestPostgresEventStore_Append_ResolvesClientQueryNormFromSearch(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	store := persistence.NewPostgresEventStore(pool, clock.Now)
	owner := newEventTestUser(t.Context(), t, pool)
	other := newEventTestUser(t.Context(), t, pool)

	suffix := uuid.New().String()[:8]
	canonical := "qn canonical " + suffix
	searchID := uuid.New().String()
	performSearch(t, store, owner, canonical, searchID)

	cases := []struct {
		name     string
		userId   ids.UserId
		searchID string
		want     *string
	}{
		{"own search overrides client value", owner, searchID, &canonical},
		{"another user's search resolves nothing", other, searchID, nil},
		{"unknown search resolves nothing", owner, uuid.New().String(), nil},
		{"no search resolves nothing", owner, "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eventID := uuid.New().String()
			appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
				UserId: tc.userId, Type: domain.InteractionTypePlay,
				SearchId: tc.searchID, EventId: eventID, QueryNorm: "forged " + suffix,
			})
			got := storedQueryNorm(t, pool, tc.userId, eventID)
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Errorf("stored query_norm = %v, want %v", deref(got), deref(tc.want))
			}
		})
	}
}

func deref(s *string) string {
	if s == nil {
		return "<NULL>"
	}
	return *s
}

func appendOrFatal(ctx context.Context, t *testing.T, store *persistence.PostgresEventStore, ev domain.InteractionEvent) {
	t.Helper()
	if err := store.Append(ctx, ev); err != nil {
		t.Fatalf("Append(%s): %v", ev.Type.String(), err)
	}
}

func newEventTestUser(ctx context.Context, t *testing.T, pool *pgxpool.Pool) ids.UserId {
	t.Helper()
	userId := ids.NewUserId(uuid.New())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.WithoutCancel(ctx),
			`DELETE FROM discovery_events WHERE user_id = $1`, userId.UUID())
	})
	return userId
}

func showResults(t *testing.T, store *persistence.PostgresEventStore, userId ids.UserId, occurredAt time.Time, sigs ...string) {
	t.Helper()
	appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
		OccurredAt: occurredAt,
		UserId:     userId, Type: domain.InteractionTypeSearchPerformed,
		SearchId:  uuid.New().String(),
		QueryNorm: "shown",
		Payload:   map[string]any{"shown_signatures": sigs},
	})
}

func TestPostgresEventStore_SatisfactionSignals_RequiresShownResult(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	store := persistence.NewPostgresEventStore(pool, clock.Now)
	ctx := context.Background()
	now := time.Now().UTC()

	attacker := newEventTestUser(t.Context(), t, pool)
	viewer := newEventTestUser(t.Context(), t, pool)
	stale := newEventTestUser(t.Context(), t, pool)
	honest := newEventTestUser(t.Context(), t, pool)

	suffix := uuid.New().String()[:8]
	sigForged := "track|forged title " + suffix + "|forged artist"
	sigLegit := "track|legit title " + suffix + "|legit artist"

	showResults(t, store, viewer, now, "track|other "+suffix+"|x")
	showResults(t, store, attacker, now, sigLegit)
	for _, typ := range []domain.InteractionType{domain.InteractionTypePlay, domain.InteractionTypeCompleted} {
		appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
			UserId: attacker, Type: typ,
			Payload: map[string]any{"result_signature": sigForged},
		})
	}
	showResults(t, store, viewer, now, sigForged)
	appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
		UserId: attacker, Type: domain.InteractionTypePlay,
		Payload: map[string]any{"result_signature": sigForged},
	})

	const shownResultWindow = 24 * time.Hour
	showResults(t, store, stale, now.Add(-shownResultWindow-time.Hour), sigForged)
	appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
		UserId: stale, Type: domain.InteractionTypePlay,
		Payload: map[string]any{"result_signature": sigForged},
	})

	showResults(t, store, honest, now.Add(-time.Minute), sigLegit)
	appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
		UserId: honest, Type: domain.InteractionTypeCompleted,
		Payload: map[string]any{"result_signature": sigLegit},
	})

	signals, err := store.SatisfactionSignals(ctx, now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("SatisfactionSignals: %v", err)
	}
	scores := map[string]float64{}
	for _, s := range signals {
		scores[s.ResultSignature] = s.Score
	}
	if got, present := scores[sigForged]; present {
		t.Errorf("forged signature score = %v, want excluded (never shown to the submitting user in a recent search)", got)
	}
	if got := scores[sigLegit]; got != 1 {
		t.Errorf("legit signature score = %v, want 1 (completed after being shown)", got)
	}
}

func TestPostgresEventStore_SatisfactionSignals(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	store := persistence.NewPostgresEventStore(pool, clock.Now)
	ctx := context.Background()

	userA := newEventTestUser(t.Context(), t, pool)
	userB := newEventTestUser(t.Context(), t, pool)
	userC := newEventTestUser(t.Context(), t, pool)

	suffix := uuid.New().String()[:8]
	sigCapped := "sig-cap-" + suffix
	sigMixed := "sig-mixed-" + suffix
	sigPoisoned := "sig-poison-" + suffix
	sigZeroNet := "sig-zeronet-" + suffix

	shownAt := time.Now().UTC().Add(-time.Second)
	showResults(t, store, userA, shownAt, sigCapped, sigMixed)
	showResults(t, store, userB, shownAt, sigMixed, sigPoisoned)
	showResults(t, store, userC, shownAt, sigZeroNet)

	for range 10 {
		appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
			UserId: userA, Type: domain.InteractionTypePlay,
			Payload: map[string]any{"result_signature": sigCapped},
		})
	}

	appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
		UserId: userA, Type: domain.InteractionTypePlay,
		Payload: map[string]any{"result_signature": sigMixed},
	})
	appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
		UserId: userA, Type: domain.InteractionTypeCompleted,
		Payload: map[string]any{"result_signature": sigMixed},
	})
	appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
		UserId: userB, Type: domain.InteractionTypeSkip,
		Payload: map[string]any{"result_signature": sigMixed, "dwell_ms": 1500},
	})

	appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
		UserId: userB, Type: domain.InteractionTypePlay,
		Payload: map[string]any{"result_signature": sigPoisoned},
	})
	for _, dwell := range []any{"abc", map[string]any{"ms": 5}, nil, 25000} {
		payload := map[string]any{"result_signature": sigPoisoned}
		if dwell != nil {
			payload["dwell_ms"] = dwell
		}
		appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
			UserId: userB, Type: domain.InteractionTypeSkip, Payload: payload,
		})
	}

	appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
		UserId: userC, Type: domain.InteractionTypePlay,
		Payload: map[string]any{"result_signature": sigZeroNet},
	})
	appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
		UserId: userC, Type: domain.InteractionTypeSkip,
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

func TestPostgresEventStore_ZeroResultQueries(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	store := persistence.NewPostgresEventStore(pool, clock.Now)
	ctx := context.Background()
	userId := newEventTestUser(t.Context(), t, pool)

	suffix := uuid.New().String()[:8]
	qHit := "qa zero hit " + suffix
	qPoison := "qa zero poison " + suffix
	qAbsent := "qa zero absent " + suffix
	qFalse := "qa zero false " + suffix

	search := func(queryNorm string, payload map[string]any) {
		appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
			UserId: userId, Type: domain.InteractionTypeSearchPerformed,
			QueryNorm: queryNorm, Payload: payload,
		})
	}
	search(qHit, map[string]any{"zero_result": true})
	search(qHit, map[string]any{"zero_result": true})
	search(qPoison, map[string]any{"zero_result": "yes"})
	search(qAbsent, map[string]any{})
	search(qFalse, map[string]any{"zero_result": false})

	counts, err := store.NoResultSearches(ctx, time.Now().UTC().Add(-time.Minute), 1000)
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

func TestPostgresEventStore_ZeroResultTotal(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	store := persistence.NewPostgresEventStore(pool, clock.Now)
	ctx := context.Background()
	userId := newEventTestUser(t.Context(), t, pool)

	suffix := uuid.New().String()[:8]
	since := time.Now().UTC().Add(-time.Minute)

	search := func(queryNorm string, payload map[string]any) {
		appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
			UserId: userId, Type: domain.InteractionTypeSearchPerformed,
			QueryNorm: queryNorm, Payload: payload,
		})
	}

	const distinct = 1100
	for i := range distinct {
		search(fmt.Sprintf("qa total %s %d", suffix, i), map[string]any{"zero_result": true})
	}
	search("qa total poison "+suffix, map[string]any{"zero_result": "yes"})
	search("qa total absent "+suffix, map[string]any{})
	search("qa total false "+suffix, map[string]any{"zero_result": false})

	total, err := store.ZeroResultTotal(ctx, since)
	if err != nil {
		t.Fatalf("ZeroResultTotal: %v", err)
	}
	if total != distinct {
		t.Errorf("ZeroResultTotal = %d, want %d (unbounded count, no top-N cap, noise excluded)", total, distinct)
	}

	capped, err := store.NoResultSearches(ctx, since, 1000)
	if err != nil {
		t.Fatalf("ZeroResultQueries: %v", err)
	}
	cappedSum := 0
	for _, qc := range capped {
		cappedSum += qc.Count
	}
	if cappedSum >= total {
		t.Errorf("capped sum %d >= true total %d; cap should have dropped rows", cappedSum, total)
	}
}

func TestPostgresEventStore_NonZeroNoClickQueries(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	store := persistence.NewPostgresEventStore(pool, clock.Now)
	ctx := context.Background()
	userId := newEventTestUser(t.Context(), t, pool)

	suffix := uuid.New().String()[:8]
	qNoClick := "qa noclick " + suffix
	qClicked := "qa clicked " + suffix
	qZero := "qa noclick zero " + suffix
	qPoison := "qa noclick poison " + suffix

	appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.InteractionTypeSearchPerformed,
		QueryNorm: qNoClick, Payload: map[string]any{"zero_result": false},
	})
	clickedSearchID := uuid.New().String()
	appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.InteractionTypeSearchPerformed, SearchId: clickedSearchID,
		QueryNorm: qClicked, Payload: map[string]any{"zero_result": false},
	})
	appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.InteractionTypeResultClicked, SearchId: clickedSearchID,
		Payload: map[string]any{"result_signature": "s"},
	})
	appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.InteractionTypeSearchPerformed,
		QueryNorm: qZero, Payload: map[string]any{"zero_result": true},
	})
	appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.InteractionTypeSearchPerformed,
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

func TestPostgresEventStore_AbandonedSearches(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	store := persistence.NewPostgresEventStore(pool, clock.Now)
	ctx := context.Background()

	suffix := uuid.New().String()[:8]
	base := time.Now().UTC().Add(-10 * time.Minute)

	search := func(userId ids.UserId, queryNorm, searchID string, at time.Time) {
		appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
			UserId: userId, Type: domain.InteractionTypeSearchPerformed,
			QueryNorm: queryNorm, SearchId: searchID, OccurredAt: at,
		})
	}

	userA := newEventTestUser(t.Context(), t, pool)
	qAband := "qa aband " + suffix
	search(userA, qAband, "", base)
	search(userA, "zz retry one "+suffix, "", base.Add(30*time.Second))

	userB := newEventTestUser(t.Context(), t, pool)
	q59 := "qa aband 59s " + suffix
	search(userB, q59, "", base)
	search(userB, "zz retry two "+suffix, "", base.Add(59*time.Second))

	userC := newEventTestUser(t.Context(), t, pool)
	q61 := "qa aband 61s " + suffix
	search(userC, q61, "", base)
	search(userC, "zz retry three "+suffix, "", base.Add(61*time.Second))

	userD := newEventTestUser(t.Context(), t, pool)
	qClicked := "qa aband clicked " + suffix
	clickedSearchID := uuid.New().String()
	search(userD, qClicked, clickedSearchID, base)
	appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
		UserId: userD, Type: domain.InteractionTypeResultClicked, SearchId: clickedSearchID,
		OccurredAt: base.Add(5 * time.Second),
		Payload:    map[string]any{"result_signature": "s"},
	})
	search(userD, "zz retry four "+suffix, "", base.Add(30*time.Second))

	userE, userF := newEventTestUser(t.Context(), t, pool), newEventTestUser(t.Context(), t, pool)
	qAlone := "qa aband alone " + suffix
	search(userE, qAlone, "", base)
	search(userF, "qa aband other user "+suffix, "", base.Add(10*time.Second))

	userG := newEventTestUser(t.Context(), t, pool)
	qTyping := "qa aband typ " + suffix[:4]
	search(userG, qTyping, "", base)
	search(userG, qTyping+suffix[4:], "", base.Add(2*time.Second))

	counts, err := store.AbandonedSearches(ctx, base.Add(-time.Minute), 1000)
	if err != nil {
		t.Fatalf("AbandonedSearches: %v", err)
	}
	got := map[string]int{}
	for _, qc := range counts {
		got[qc.QueryNorm] = qc.Count
	}

	if got[qAband] != 1 {
		t.Errorf("count(%q) = %d, want 1 (no click + the same user searched again 30s later)", qAband, got[qAband])
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
	if _, present := got[qTyping]; present {
		t.Errorf("%q counted as abandoned, want excluded (the next search only kept typing it)", qTyping)
	}
	if _, present := got[qAlone]; present {
		t.Errorf("%q counted as abandoned, want excluded (only another user searched next)", qAlone)
	}
}

func TestPostgresEventStore_Append_EventIDDedupAndMalformedIDs(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	store := persistence.NewPostgresEventStore(pool, clock.Now)
	ctx := context.Background()

	countRows := func(t *testing.T, userId ids.UserId) int {
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
		userId := newEventTestUser(t.Context(), t, pool)
		eventID := uuid.New().String()
		ev := domain.InteractionEvent{
			UserId: userId, Type: domain.InteractionTypeLibraryAdd, EventId: eventID,
			Payload: map[string]any{"result_signature": "sig-dedup"},
		}
		appendOrFatal(t.Context(), t, store, ev)
		appendOrFatal(t.Context(), t, store, ev)
		if n := countRows(t, userId); n != 1 {
			t.Errorf("rows after retried critical event = %d, want 1 (ON CONFLICT no-op)", n)
		}
	})

	t.Run("another user's identical event_id still inserts", func(t *testing.T) {
		replayer := newEventTestUser(t.Context(), t, pool)
		victim := newEventTestUser(t.Context(), t, pool)
		eventID := uuid.New().String()

		appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
			UserId: replayer, Type: domain.InteractionTypeLibraryAdd, EventId: eventID,
			Payload: map[string]any{"result_signature": "sig-replayed"},
		})
		appendOrFatal(t.Context(), t, store, domain.InteractionEvent{
			UserId: victim, Type: domain.InteractionTypeLibraryAdd, EventId: eventID,
			Payload: map[string]any{"result_signature": "sig-own"},
		})

		if n := countRows(t, victim); n != 1 {
			t.Errorf("victim rows = %d, want 1 (another user's event_id must not swallow it)", n)
		}
	})

	t.Run("malformed event_id inserts without dedup", func(t *testing.T) {
		userId := newEventTestUser(ctx, t, pool)
		ev := domain.InteractionEvent{
			UserId: userId, Type: domain.InteractionTypeLibraryAdd, EventId: "not-a-uuid",
		}
		appendOrFatal(ctx, t, store, ev)
		appendOrFatal(ctx, t, store, ev)
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
		userId := newEventTestUser(ctx, t, pool)
		appendOrFatal(ctx, t, store, domain.InteractionEvent{
			UserId: userId, Type: domain.InteractionTypePlay, SearchId: "bogus-not-a-uuid",
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
