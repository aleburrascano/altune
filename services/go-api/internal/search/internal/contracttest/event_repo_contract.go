package contracttest

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/ids"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

type EventRepoUnderTest interface {
	ports.EventStore
	NoResultSearches(ctx context.Context, since time.Time, limit int) ([]ports.QueryCount, error)
	NonZeroNoClickQueries(ctx context.Context, since time.Time, limit int) ([]ports.QueryCount, error)
	AbandonedSearches(ctx context.Context, since time.Time, limit int) ([]ports.QueryCount, error)
	NewUser(t *testing.T) ids.UserId
}

const eventQueryLimit = 1000

func EventRepoContract(t *testing.T, newRepo func(t *testing.T) EventRepoUnderTest) {
	t.Helper()
	contracts := map[string]func(*testing.T, EventRepoUnderTest){
		"no-result searches are counted per query, most frequent first": contractNoResultSearchesAreCounted,
		"a search that found results is not a no-result search":         contractFoundSearchIsNotNoResult,
		"a search older than the window is not counted":                 contractOldSearchIsOutsideTheWindow,
		"an event appended twice under one id is counted once":          contractEventIdIsIdempotent,
		"a found search nobody clicked is reported":                     contractUnclickedSearchIsReported,
		"a found search with a click on it is not reported":             contractClickedSearchIsNotReported,

		"an unclicked search followed by an unrelated search inside the minute is abandoned": contractUnclickedSearchFollowedByAnotherIsAbandoned,
		"a follow-up 59 seconds later counts and one 61 seconds later does not":              contractAbandonedFollowUpWindowIsSixtySeconds,
		"a clicked search is not abandoned":                                                  contractClickedSearchIsNotAbandoned,
		"a follow-up that only keeps typing the query is not abandonment":                    contractRefinedQueryIsNotAbandonment,
		"a follow-up by another user is not abandonment":                                     contractOtherUsersSearchIsNotAbandonment,
	}
	for name, contract := range contracts {
		t.Run(name, func(t *testing.T) { contract(t, newRepo(t)) })
	}
}

func uniqueQuery() string { return "contract-" + uuid.NewString() }

func appendEventOrFail(t *testing.T, r EventRepoUnderTest, event domain.InteractionEvent) {
	t.Helper()
	if err := r.Append(t.Context(), event); err != nil {
		t.Fatalf("Append: %v", err)
	}
}

func searchEvent(user ids.UserId, query string, zeroResult bool, at time.Time) domain.InteractionEvent {
	return domain.InteractionEvent{
		UserId:     user,
		Type:       domain.InteractionTypeSearchPerformed,
		QueryNorm:  query,
		SearchId:   uuid.NewString(),
		OccurredAt: at,
		Payload:    map[string]any{domain.PayloadKeyZeroResult: zeroResult},
	}
}

func countsFor(t *testing.T, counts []ports.QueryCount, err error, queries ...string) map[string]int {
	t.Helper()
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	mine := map[string]int{}
	for _, c := range counts {
		for _, q := range queries {
			if c.QueryNorm == q {
				mine[q] = c.Count
			}
		}
	}
	return mine
}

const eventWindowSpan = time.Hour

func eventWindow() time.Time { return time.Now().Add(-eventWindowSpan) }

func contractNoResultSearchesAreCounted(t *testing.T, r EventRepoUnderTest) {
	user, now := r.NewUser(t), time.Now()
	rare, common := uniqueQuery(), uniqueQuery()
	appendEventOrFail(t, r, searchEvent(user, rare, true, now))
	for range 2 {
		appendEventOrFail(t, r, searchEvent(user, common, true, now))
	}

	counts, err := r.NoResultSearches(t.Context(), eventWindow(), eventQueryLimit)

	if got := countsFor(t, counts, err, rare, common); got[common] != 2 || got[rare] != 1 {
		t.Errorf("counts = %v, want %s:2 and %s:1", got, common, rare)
	}
	if first, second := positionOf(counts, common), positionOf(counts, rare); first > second {
		t.Errorf("%s ranks after %s, want the more frequent query first", common, rare)
	}
}

func positionOf(counts []ports.QueryCount, query string) int {
	for i, c := range counts {
		if c.QueryNorm == query {
			return i
		}
	}
	return len(counts)
}

func contractFoundSearchIsNotNoResult(t *testing.T, r EventRepoUnderTest) {
	user, query := r.NewUser(t), uniqueQuery()
	appendEventOrFail(t, r, searchEvent(user, query, false, time.Now()))

	counts, err := r.NoResultSearches(t.Context(), eventWindow(), eventQueryLimit)

	if got := countsFor(t, counts, err, query); len(got) != 0 {
		t.Errorf("counts = %v, want %s absent", got, query)
	}
}

func contractOldSearchIsOutsideTheWindow(t *testing.T, r EventRepoUnderTest) {
	user, query := r.NewUser(t), uniqueQuery()
	appendEventOrFail(t, r, searchEvent(user, query, true, time.Now().Add(-2*eventWindowSpan)))

	counts, err := r.NoResultSearches(t.Context(), eventWindow(), eventQueryLimit)

	if got := countsFor(t, counts, err, query); len(got) != 0 {
		t.Errorf("counts = %v, want %s absent", got, query)
	}
}

func contractEventIdIsIdempotent(t *testing.T, r EventRepoUnderTest) {
	user, query := r.NewUser(t), uniqueQuery()
	event := searchEvent(user, query, true, time.Now())
	event.EventId = uuid.NewString()

	appendEventOrFail(t, r, event)
	appendEventOrFail(t, r, event)

	counts, err := r.NoResultSearches(t.Context(), eventWindow(), eventQueryLimit)
	if got := countsFor(t, counts, err, query); got[query] != 1 {
		t.Errorf("counts = %v, want %s counted once", got, query)
	}
}

func contractUnclickedSearchIsReported(t *testing.T, r EventRepoUnderTest) {
	user, query := r.NewUser(t), uniqueQuery()
	appendEventOrFail(t, r, searchEvent(user, query, false, time.Now()))

	counts, err := r.NonZeroNoClickQueries(t.Context(), eventWindow(), eventQueryLimit)

	if got := countsFor(t, counts, err, query); got[query] != 1 {
		t.Errorf("counts = %v, want %s reported once", got, query)
	}
}

func contractClickedSearchIsNotReported(t *testing.T, r EventRepoUnderTest) {
	user, query := r.NewUser(t), uniqueQuery()
	search := searchEvent(user, query, false, time.Now())
	appendEventOrFail(t, r, search)
	appendEventOrFail(t, r, domain.InteractionEvent{
		UserId:     user,
		Type:       domain.InteractionTypeResultClicked,
		SearchId:   search.SearchId,
		OccurredAt: time.Now(),
	})

	counts, err := r.NonZeroNoClickQueries(t.Context(), eventWindow(), eventQueryLimit)

	if got := countsFor(t, counts, err, query); len(got) != 0 {
		t.Errorf("counts = %v, want %s absent once clicked", got, query)
	}
}

func abandonedCounts(t *testing.T, r EventRepoUnderTest, queries ...string) map[string]int {
	t.Helper()
	counts, err := r.AbandonedSearches(t.Context(), eventWindow(), eventQueryLimit)
	return countsFor(t, counts, err, queries...)
}

func contractUnclickedSearchFollowedByAnotherIsAbandoned(t *testing.T, r EventRepoUnderTest) {
	user, first, next, at := r.NewUser(t), uniqueQuery(), uniqueQuery(), time.Now().Add(-10*time.Minute)
	appendEventOrFail(t, r, searchEvent(user, first, false, at))
	appendEventOrFail(t, r, searchEvent(user, next, false, at.Add(30*time.Second)))

	if got := abandonedCounts(t, r, first, next); got[first] != 1 || len(got) != 1 {
		t.Errorf("counts = %v, want only %s abandoned, once", got, first)
	}
}

func contractAbandonedFollowUpWindowIsSixtySeconds(t *testing.T, r EventRepoUnderTest) {
	at := time.Now().Add(-10 * time.Minute)
	inside, outside := uniqueQuery(), uniqueQuery()
	for query, gap := range map[string]time.Duration{inside: 59 * time.Second, outside: 61 * time.Second} {
		user := r.NewUser(t)
		appendEventOrFail(t, r, searchEvent(user, query, false, at))
		appendEventOrFail(t, r, searchEvent(user, uniqueQuery(), false, at.Add(gap)))
	}

	got := abandonedCounts(t, r, inside, outside)

	if got[inside] != 1 {
		t.Errorf("counts = %v, want %s abandoned at 59s", got, inside)
	}
	if _, present := got[outside]; present {
		t.Errorf("counts = %v, want %s absent at 61s", got, outside)
	}
}

func contractClickedSearchIsNotAbandoned(t *testing.T, r EventRepoUnderTest) {
	user, query, at := r.NewUser(t), uniqueQuery(), time.Now().Add(-10*time.Minute)
	search := searchEvent(user, query, false, at)
	appendEventOrFail(t, r, search)
	appendEventOrFail(t, r, domain.InteractionEvent{
		UserId:     user,
		Type:       domain.InteractionTypeResultClicked,
		SearchId:   search.SearchId,
		OccurredAt: at.Add(5 * time.Second),
	})
	appendEventOrFail(t, r, searchEvent(user, uniqueQuery(), false, at.Add(30*time.Second)))

	if got := abandonedCounts(t, r, query); len(got) != 0 {
		t.Errorf("counts = %v, want %s absent once clicked", got, query)
	}
}

func contractRefinedQueryIsNotAbandonment(t *testing.T, r EventRepoUnderTest) {
	user, query, at := r.NewUser(t), uniqueQuery(), time.Now().Add(-10*time.Minute)
	appendEventOrFail(t, r, searchEvent(user, query, false, at))
	appendEventOrFail(t, r, searchEvent(user, query+"-more", false, at.Add(2*time.Second)))

	if got := abandonedCounts(t, r, query); len(got) != 0 {
		t.Errorf("counts = %v, want %s absent when the next search only extends it", got, query)
	}
}

func contractOtherUsersSearchIsNotAbandonment(t *testing.T, r EventRepoUnderTest) {
	query, at := uniqueQuery(), time.Now().Add(-10*time.Minute)
	appendEventOrFail(t, r, searchEvent(r.NewUser(t), query, false, at))
	appendEventOrFail(t, r, searchEvent(r.NewUser(t), uniqueQuery(), false, at.Add(10*time.Second)))

	if got := abandonedCounts(t, r, query); len(got) != 0 {
		t.Errorf("counts = %v, want %s absent when only another user searched next", got, query)
	}
}
