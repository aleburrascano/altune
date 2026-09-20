//go:build integration

package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// performSearch records the server-emitted search_performed row that owns the
// canonical query_norm for searchID.
func performSearch(t *testing.T, store *PgxEventStore, userId shared.UserId, queryNorm, searchID string) {
	t.Helper()
	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: userId, Type: domain.EventTypeSearchPerformed,
		QueryNorm: queryNorm, SearchId: searchID,
		Payload: map[string]any{"zero_result": false},
	})
}

// storedQueryNorm reads the row by (user, event_id): since #2245 the event_id
// key is per-user, so an id alone can name a row belonging to someone else.
func storedQueryNorm(t *testing.T, store *PgxEventStore, userId shared.UserId, eventID string) *string {
	t.Helper()
	var queryNorm *string
	if err := store.pool.QueryRow(context.Background(),
		`SELECT query_norm FROM discovery_events WHERE user_id = $1 AND event_id = $2`,
		userId.UUID(), uuid.MustParse(eventID),
	).Scan(&queryNorm); err != nil {
		t.Fatalf("read stored query_norm: %v", err)
	}
	return queryNorm
}

// Regression for #1086: a client-submitted event's query_norm is never
// trusted. It is resolved from the submitting user's own search_performed row
// for the event's search_id, so a garbled or forged value cannot hide a real
// click from, or plant a fake click into, the no-click coverage-gap signal.
func TestPgxEventStore_NonZeroNoClickQueries_IgnoresClientQueryNorm(t *testing.T) {
	store := NewPgxEventStore(testPool(t))
	ctx := context.Background()
	honest := newEventTestUser(t, store)
	attacker := newEventTestUser(t, store)

	suffix := uuid.New().String()[:8]
	qAnswered := "qn answered " + suffix
	qTarget := "qn target gap " + suffix
	answeredSearch := uuid.New().String()

	performSearch(t, store, honest, qAnswered, answeredSearch)
	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: honest, Type: domain.EventTypeResultClicked,
		SearchId: answeredSearch, QueryNorm: "garbled " + suffix,
		Payload: map[string]any{"result_signature": "s"},
	})

	performSearch(t, store, honest, qTarget, uuid.New().String())
	attackerSearch := uuid.New().String()
	performSearch(t, store, attacker, "qn attacker own "+suffix, attackerSearch)
	for _, searchID := range []string{"", attackerSearch} {
		appendOrFatal(t, store, domain.InteractionEvent{
			UserId: attacker, Type: domain.EventTypeResultClicked,
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

func TestPgxEventStore_Append_ResolvesClientQueryNormFromSearch(t *testing.T) {
	store := NewPgxEventStore(testPool(t))
	owner := newEventTestUser(t, store)
	other := newEventTestUser(t, store)

	suffix := uuid.New().String()[:8]
	canonical := "qn canonical " + suffix
	searchID := uuid.New().String()
	performSearch(t, store, owner, canonical, searchID)

	cases := []struct {
		name     string
		userId   shared.UserId
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
			appendOrFatal(t, store, domain.InteractionEvent{
				UserId: tc.userId, Type: domain.EventTypePlay,
				SearchId: tc.searchID, EventId: eventID, QueryNorm: "forged " + suffix,
			})
			got := storedQueryNorm(t, store, tc.userId, eventID)
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
