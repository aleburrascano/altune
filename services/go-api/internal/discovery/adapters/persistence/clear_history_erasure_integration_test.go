//go:build integration

package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newClearHistoryTestUser is an account whose rows in both stores the clear
// touches are removed afterwards, so a failing assertion leaves nothing behind
// for the next test to count.
func newClearHistoryTestUser(t *testing.T, pool *pgxpool.Pool) shared.UserId {
	t.Helper()
	owner := newEventTestUser(t, NewPgxEventStore(pool))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM discovery_search_history WHERE user_id = $1`, owner.UUID())
	})
	return owner
}

// seedSearchTextOf runs one search for owner through the real write paths and
// returns its search_id. It leaves the query text in all three places the clear
// has to reach: the history row, the server-emitted search_performed event that
// owns the text, and a derived click Append stamps with that same text.
func seedSearchTextOf(t *testing.T, pool *pgxpool.Pool, owner shared.UserId, queryNorm string) string {
	t.Helper()
	store := NewPgxEventStore(pool)
	searchID := uuid.New().String()
	performSearch(t, store, owner, queryNorm, searchID)
	appendOrFatal(t, store, domain.InteractionEvent{
		UserId: owner, Type: domain.EventTypeResultClicked, SearchId: searchID,
		Payload: map[string]any{domain.PayloadKeyResultSignature: "sig " + queryNorm},
	})
	seedHistoryEntry(t, NewPgxSearchHistoryRepository(pool), owner, queryNorm, time.Now().UTC())
	return searchID
}

func storedSearchTextsOf(t *testing.T, pool *pgxpool.Pool, owner shared.UserId) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT query_norm FROM discovery_events WHERE user_id = $1 AND query_norm IS NOT NULL`,
		owner.UUID())
	if err != nil {
		t.Fatalf("read stored search text: %v", err)
	}
	defer rows.Close()
	texts, err := collectRows(rows, func(rows pgx.Rows) (string, error) {
		var text string
		return text, rows.Scan(&text)
	})
	if err != nil {
		t.Fatalf("scan stored search text: %v", err)
	}
	return texts
}

func clearSearchHistoryOf(t *testing.T, pool *pgxpool.Pool, owner shared.UserId) {
	t.Helper()
	svc := service.NewClearSearchHistoryService(NewPgxSearchHistoryRepository(pool))
	if err := svc.Execute(context.Background(), owner); err != nil {
		t.Fatalf("ClearSearchHistoryService.Execute: %v", err)
	}
}

// TestClearSearchHistory_ErasesTheSearchTextTelemetryKept is the guard for
// #2237: the clear emptied discovery_search_history only, so an account that
// asked to be forgotten still had every query it ran sitting in
// discovery_events under its own user_id for the retention window. Both
// accounts go through the one clear, because what has to hold is that it tells
// them apart — erasing the other account's text is the half that cannot be
// undone.
func TestClearSearchHistory_ErasesTheSearchTextTelemetryKept(t *testing.T) {
	pool := testPool(t)
	clearing := newClearHistoryTestUser(t, pool)
	keeping := newClearHistoryTestUser(t, pool)
	suffix := uuid.New().String()[:8]
	seedSearchTextOf(t, pool, clearing, "cleared query "+suffix)
	seedSearchTextOf(t, pool, keeping, "kept query "+suffix)

	clearSearchHistoryOf(t, pool, clearing)

	if got := storedSearchTextsOf(t, pool, clearing); len(got) != 0 {
		t.Errorf("telemetry still names %v for the account that cleared its history, want none", got)
	}
	if got := countOwnedRows(t, pool,
		`SELECT COUNT(*) FROM discovery_search_history WHERE user_id = $1`, clearing); got != 0 {
		t.Errorf("history rows after the clear = %d, want 0", got)
	}
	if got := storedSearchTextsOf(t, pool, keeping); len(got) != 2 {
		t.Errorf("another account's stored search text = %v, want its 2 rows untouched", got)
	}
	// Blanked rather than deleted, so the signals that need a count rather than
	// the text — satisfaction scores, the discography aggregate — keep reading
	// the same rows.
	if got := countOwnedRows(t, pool,
		`SELECT COUNT(*) FROM discovery_events WHERE user_id = $1`, clearing); got != 2 {
		t.Errorf("events after the clear = %d, want 2 (blanked, not deleted)", got)
	}
}

// TestClearSearchHistory_ALateEventCannotRestoreTheErasedText is the mobile
// outbox flushing events it queued before the clear. Append resolves a derived
// event's query_norm from the account's own search_performed row, which the
// clear blanked, so a replay lands with no text instead of re-planting it; and
// search_performed is not client-submittable, so no client can set the column
// itself.
func TestClearSearchHistory_ALateEventCannotRestoreTheErasedText(t *testing.T) {
	pool := testPool(t)
	owner := newClearHistoryTestUser(t, pool)
	suffix := uuid.New().String()[:8]
	queryNorm := "late flush " + suffix
	searchID := seedSearchTextOf(t, pool, owner, queryNorm)
	clearSearchHistoryOf(t, pool, owner)

	appendOrFatal(t, NewPgxEventStore(pool), domain.InteractionEvent{
		UserId: owner, Type: domain.EventTypePlay, SearchId: searchID,
		QueryNorm: queryNorm,
		Payload:   map[string]any{domain.PayloadKeyResultSignature: "sig " + suffix},
	})

	if got := storedSearchTextsOf(t, pool, owner); len(got) != 0 {
		t.Errorf("an event flushed after the clear restored %v, want no stored search text", got)
	}
}
