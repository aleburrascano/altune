package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service/history"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedHistoryEntry(t *testing.T, repo *PgxSearchHistoryRepository, userId shared.UserId, query string, at time.Time) {
	t.Helper()
	entry := &domain.SearchHistoryEntry{
		ID:         shared.NewEntityId(),
		UserId:     userId,
		Query:      query,
		QueryNorm:  query,
		ExecutedAt: at,
	}
	if err := repo.Insert(context.Background(), entry); err != nil {
		t.Fatalf("Insert(%q): %v", query, err)
	}
}

func TestPgxSearchHistoryRepo_EraseSearchTextForUser(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := testPool(t)
	repo := NewPgxSearchHistoryRepository(pool)
	ctx := context.Background()
	userA := shared.NewUserId(uuid.New())
	userB := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		for _, u := range []shared.UserId{userA, userB} {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM discovery_search_history WHERE user_id = $1`, u.UUID())
		}
	})

	suffix := uuid.New().String()[:8]
	now := time.Now().UTC()
	seedHistoryEntry(t, repo, userA, "hist a1 "+suffix, now.Add(-3*time.Minute))
	seedHistoryEntry(t, repo, userA, "hist a2 "+suffix, now.Add(-2*time.Minute))
	seedHistoryEntry(t, repo, userA, "hist a3 "+suffix, now.Add(-1*time.Minute))
	seedHistoryEntry(t, repo, userB, "hist b1 "+suffix, now.Add(-1*time.Minute))

	if err := repo.EraseSearchTextForUser(ctx, userA); err != nil {
		t.Fatalf("EraseSearchTextForUser: %v", err)
	}

	gotA, err := repo.ListDistinctRecent(ctx, userA, 10)
	if err != nil {
		t.Fatalf("ListDistinctRecent(userA): %v", err)
	}
	if len(gotA) != 0 {
		t.Errorf("userA entries after delete = %d, want 0", len(gotA))
	}

	gotB, err := repo.ListDistinctRecent(ctx, userB, 10)
	if err != nil {
		t.Fatalf("ListDistinctRecent(userB): %v", err)
	}
	if len(gotB) != 1 {
		t.Errorf("userB entries after userA delete = %d, want 1 (scoped delete)", len(gotB))
	}

	if err := repo.EraseSearchTextForUser(ctx, userA); err != nil {
		t.Errorf("EraseSearchTextForUser on empty history: %v, want nil", err)
	}
}

func TestPgxSearchHistoryRepo_ListDistinctRecent_LimitBoundary(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := testPool(t)
	repo := NewPgxSearchHistoryRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM discovery_search_history WHERE user_id = $1`, userId.UUID())
	})

	suffix := uuid.New().String()[:8]
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		seedHistoryEntry(t, repo, userId, "limit q"+string(rune('a'+i))+" "+suffix,
			now.Add(time.Duration(i)*time.Second))
	}

	got, err := repo.ListDistinctRecent(ctx, userId, 2)
	if err != nil {
		t.Fatalf("ListDistinctRecent: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (limit)", len(got))
	}
	if got[0].QueryNorm != "limit qc "+suffix || got[1].QueryNorm != "limit qb "+suffix {
		t.Errorf("got [%q, %q], want the two most recent [%q, %q]",
			got[0].QueryNorm, got[1].QueryNorm, "limit qc "+suffix, "limit qb "+suffix)
	}
}

func newClearHistoryTestUser(t *testing.T, pool *pgxpool.Pool) shared.UserId {
	t.Helper()
	owner := newEventTestUser(t, NewPgxEventStore(pool))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM discovery_search_history WHERE user_id = $1`, owner.UUID())
	})
	return owner
}

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
	svc := history.NewClearSearchHistoryService(NewPgxSearchHistoryRepository(pool))
	if err := svc.Execute(context.Background(), owner); err != nil {
		t.Fatalf("ClearSearchHistoryService.Execute: %v", err)
	}
}

func TestClearSearchHistory_ErasesTheSearchTextTelemetryKept(t *testing.T) {
	sharedtest.RequireIntegration(t)
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
	if got := countOwnedRows(t, pool,
		`SELECT COUNT(*) FROM discovery_events WHERE user_id = $1`, clearing); got != 2 {
		t.Errorf("events after the clear = %d, want 2 (blanked, not deleted)", got)
	}
}

func TestClearSearchHistory_ALateEventCannotRestoreTheErasedText(t *testing.T) {
	sharedtest.RequireIntegration(t)
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

func TestClearSearchHistory_ALateInsertOfASearchBegunBeforeTheClearLeavesNoRow(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := testPool(t)
	owner := newClearHistoryTestUser(t, pool)
	began := time.Now().UTC().Add(-time.Second)

	clearSearchHistoryOf(t, pool, owner)
	seedHistoryEntry(t, NewPgxSearchHistoryRepository(pool), owner, "late insert "+uuid.New().String()[:8], began)

	if got := countOwnedRows(t, pool,
		`SELECT COUNT(*) FROM discovery_search_history WHERE user_id = $1`, owner); got != 0 {
		t.Errorf("history rows after a late insert = %d, want 0", got)
	}
}

func TestClearSearchHistory_ALateSearchPerformedEventKeepsNoSearchText(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := testPool(t)
	owner := newClearHistoryTestUser(t, pool)
	began := time.Now().UTC().Add(-time.Second)

	clearSearchHistoryOf(t, pool, owner)
	appendOrFatal(t, NewPgxEventStore(pool), domain.InteractionEvent{
		UserId: owner, Type: domain.EventTypeSearchPerformed, OccurredAt: began,
		QueryNorm: "late event " + uuid.New().String()[:8], SearchId: uuid.New().String(),
		Payload: map[string]any{"zero_result": false},
	})

	if got := storedSearchTextsOf(t, pool, owner); len(got) != 0 {
		t.Errorf("a search_performed event begun before the clear stored %v, want none", got)
	}
}

func TestClearSearchHistory_ASearchBeginningAfterTheEraseStillRecords(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := testPool(t)
	owner := newClearHistoryTestUser(t, pool)
	clearSearchHistoryOf(t, pool, owner)
	query, err := domain.NewSearchQuery("after erase", domain.AllKinds(), 10)
	if err != nil {
		t.Fatalf("NewSearchQuery: %v", err)
	}

	history.NewRecordSearchHistoryService(NewPgxSearchHistoryRepository(pool)).
		Record(context.Background(), owner, query, "after erase", true, time.Now().UTC())
	appendOrFatal(t, NewPgxEventStore(pool), domain.InteractionEvent{
		UserId: owner, Type: domain.EventTypeSearchPerformed, OccurredAt: time.Now().UTC(),
		QueryNorm: "after erase", SearchId: uuid.New().String(),
		Payload: map[string]any{"zero_result": false},
	})

	if got := countOwnedRows(t, pool,
		`SELECT COUNT(*) FROM discovery_search_history WHERE user_id = $1`, owner); got != 1 {
		t.Errorf("history rows for a search begun after the clear = %d, want 1", got)
	}
	if got := storedSearchTextsOf(t, pool, owner); len(got) != 1 {
		t.Errorf("stored search text for a search begun after the clear = %v, want 1", got)
	}
}

func TestClearSearchHistory_AnInsertRacingAnOpenEraseLeavesNoSearchText(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := testPool(t)
	owner := newClearHistoryTestUser(t, pool)
	ctx := context.Background()
	began := time.Now().UTC().Add(-time.Second)
	suffix := uuid.New().String()[:8]

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin erase: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, eraseSQL := range eraseSearchTextOfUserSQL {
		if _, err := tx.Exec(ctx, eraseSQL, owner.UUID()); err != nil {
			t.Fatalf("erase step: %v", err)
		}
	}

	written := make(chan error, 2)
	go func() {
		written <- NewPgxSearchHistoryRepository(pool).Insert(ctx, &domain.SearchHistoryEntry{
			ID: shared.NewEntityId(), UserId: owner, Query: "racing " + suffix, QueryNorm: "racing " + suffix, ExecutedAt: began,
		})
	}()
	go func() {
		written <- NewPgxEventStore(pool).Append(ctx, domain.InteractionEvent{
			UserId: owner, Type: domain.EventTypeSearchPerformed, OccurredAt: began,
			QueryNorm: "racing " + suffix, SearchId: uuid.New().String(),
			Payload: map[string]any{"zero_result": false},
		})
	}()

	select {
	case err := <-written:
		t.Fatalf("a write finished while the erase was still open (err %v), want it held until the erase commits", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit erase: %v", err)
	}
	for range 2 {
		if err := <-written; err != nil {
			t.Fatalf("racing write: %v", err)
		}
	}

	if got := countOwnedRows(t, pool,
		`SELECT COUNT(*) FROM discovery_search_history WHERE user_id = $1`, owner); got != 0 {
		t.Errorf("history rows after an insert raced the open erase = %d, want 0", got)
	}
	if got := storedSearchTextsOf(t, pool, owner); len(got) != 0 {
		t.Errorf("stored search text after an append raced the open erase = %v, want none", got)
	}
}

const (
	resendWithinWindow = 10 * time.Second
	pastTheWindow      = historyResendWindow + time.Second
	concurrentResends  = 8
)

func newResendTestUser(t *testing.T, pool *pgxpool.Pool) shared.UserId {
	t.Helper()
	owner := shared.NewUserId(uuid.New())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM discovery_search_history WHERE user_id = $1`, owner.UUID())
	})
	return owner
}

func historyRowCountOf(t *testing.T, pool *pgxpool.Pool, owner shared.UserId) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM discovery_search_history WHERE user_id = $1`, owner.UUID(),
	).Scan(&count); err != nil {
		t.Fatalf("count history rows: %v", err)
	}
	return count
}

func TestPgxSearchHistoryRepo_Insert_AResendWithinTheWindowInsertsOnce(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := testPool(t)
	repo := NewPgxSearchHistoryRepository(pool)
	owner := newResendTestUser(t, pool)
	first := time.Now().UTC()

	seedHistoryEntry(t, repo, owner, "resend radiohead", first)
	seedHistoryEntry(t, repo, owner, "resend radiohead", first.Add(resendWithinWindow))

	if got := historyRowCountOf(t, pool, owner); got != 1 {
		t.Errorf("rows after a resend within the window = %d, want 1", got)
	}
}

func TestPgxSearchHistoryRepo_Insert_TheSameQueryAfterTheWindowInsertsAgain(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := testPool(t)
	repo := NewPgxSearchHistoryRepository(pool)
	owner := newResendTestUser(t, pool)
	first := time.Now().UTC().Add(-pastTheWindow)

	seedHistoryEntry(t, repo, owner, "again radiohead", first)
	seedHistoryEntry(t, repo, owner, "again radiohead", first.Add(pastTheWindow))

	if got := historyRowCountOf(t, pool, owner); got != 2 {
		t.Errorf("rows for the same query past the window = %d, want 2", got)
	}
}

func TestPgxSearchHistoryRepo_Insert_ADifferentQueryWithinTheWindowInserts(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := testPool(t)
	repo := NewPgxSearchHistoryRepository(pool)
	owner := newResendTestUser(t, pool)
	first := time.Now().UTC()

	seedHistoryEntry(t, repo, owner, "radiohead", first)
	seedHistoryEntry(t, repo, owner, "the national", first.Add(resendWithinWindow))

	if got := historyRowCountOf(t, pool, owner); got != 2 {
		t.Errorf("rows for two different queries = %d, want 2", got)
	}
}

func TestPgxSearchHistoryRepo_Insert_ConcurrentResendsOfOneQueryLeaveOneRow(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := testPool(t)
	repo := NewPgxSearchHistoryRepository(pool)
	owner := newResendTestUser(t, pool)
	at := time.Now().UTC()

	errs := make(chan error, concurrentResends)
	var wg sync.WaitGroup
	for range concurrentResends {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- repo.Insert(context.Background(), &domain.SearchHistoryEntry{
				ID: shared.NewEntityId(), UserId: owner, Query: "race radiohead", QueryNorm: "race radiohead", ExecutedAt: at,
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Insert: %v", err)
		}
	}

	if got := historyRowCountOf(t, pool, owner); got != 1 {
		t.Errorf("rows after %d concurrent resends = %d, want 1", concurrentResends, got)
	}
}
