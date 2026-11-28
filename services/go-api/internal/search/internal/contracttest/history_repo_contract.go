package contracttest

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/ids"
	"slices"
	"testing"
	"time"
)

type HistoryRepoUnderTest interface {
	ports.HistoryWriter
	ports.HistoryReader
	ports.HistoryEraser
	NewUser(t *testing.T) ids.UserId
}

func HistoryRepoContract(t *testing.T, newRepo func(t *testing.T) HistoryRepoUnderTest) {
	t.Helper()
	contracts := map[string]func(*testing.T, HistoryRepoUnderTest){
		"history lists each query once, newest first":                contractHistoryListsDistinctNewestFirst,
		"history lists no more than the limit":                       contractHistoryHonoursTheLimit,
		"the same query sent again within a minute is not recorded":  contractHistoryIgnoresAResend,
		"the same query sent again after a minute is recorded":       contractHistoryKeepsALaterRepeat,
		"trimming keeps the newest entries":                          contractHistoryTrimKeepsNewest,
		"one user's history is invisible to another":                 contractHistoryIsUserScoped,
		"erasing search text empties the history":                    contractHistoryEraseEmpties,
		"a search that started before an erase is not recorded":      contractHistoryRefusesAStaleInsert,
		"a search that starts after an erase is recorded":            contractHistoryRecordsAfterErase,
		"erasing one user's search text leaves another user's alone": contractHistoryEraseIsUserScoped,
	}
	for name, contract := range contracts {
		t.Run(name, func(t *testing.T) { contract(t, newRepo(t)) })
	}
}

const (
	historyApart      = 2 * time.Minute
	historyFarApart   = 4 * time.Minute
	historyWithinSend = 20 * time.Second
)

func historyBase() time.Time { return time.Now().Truncate(time.Second).Add(-time.Hour) }

func insertHistoryOrFail(t *testing.T, r HistoryRepoUnderTest, user ids.UserId, query string, at time.Time) {
	t.Helper()
	entry := &domain.SearchHistoryEntry{ID: ids.NewEntityId(), UserId: user, Query: query, QueryNorm: query, ExecutedAt: at}
	if err := r.Insert(t.Context(), entry); err != nil {
		t.Fatalf("Insert(%q): %v", query, err)
	}
}

func recentQueries(t *testing.T, r HistoryRepoUnderTest, user ids.UserId, limit int) []string {
	t.Helper()
	entries, err := r.ListDistinctRecent(t.Context(), user, limit)
	if err != nil {
		t.Fatalf("ListDistinctRecent: %v", err)
	}
	queries := make([]string, len(entries))
	for i, e := range entries {
		queries[i] = e.QueryNorm
	}
	return queries
}

func contractHistoryListsDistinctNewestFirst(t *testing.T, r HistoryRepoUnderTest) {
	user, base := r.NewUser(t), historyBase()
	insertHistoryOrFail(t, r, user, "a", base)
	insertHistoryOrFail(t, r, user, "b", base.Add(historyApart))
	insertHistoryOrFail(t, r, user, "a", base.Add(historyFarApart))

	if got, want := recentQueries(t, r, user, 10), []string{"a", "b"}; !slices.Equal(got, want) {
		t.Errorf("recent = %v, want %v", got, want)
	}
}

func contractHistoryHonoursTheLimit(t *testing.T, r HistoryRepoUnderTest) {
	user, base := r.NewUser(t), historyBase()
	for i, q := range []string{"a", "b", "c"} {
		insertHistoryOrFail(t, r, user, q, base.Add(time.Duration(i)*2*time.Minute))
	}

	if got, want := recentQueries(t, r, user, 2), []string{"c", "b"}; !slices.Equal(got, want) {
		t.Errorf("recent = %v, want %v", got, want)
	}
}

func contractHistoryIgnoresAResend(t *testing.T, r HistoryRepoUnderTest) {
	user, base := r.NewUser(t), historyBase()
	insertHistoryOrFail(t, r, user, "x", base)
	insertHistoryOrFail(t, r, user, "x", base.Add(historyWithinSend))

	entries, err := r.ListDistinctRecent(t.Context(), user, 10)
	if err != nil || len(entries) != 1 || !entries[0].ExecutedAt.Equal(base) {
		t.Errorf("recent = %v, %v, want the first send only", entries, err)
	}
}

func contractHistoryKeepsALaterRepeat(t *testing.T, r HistoryRepoUnderTest) {
	user, base := r.NewUser(t), historyBase()
	insertHistoryOrFail(t, r, user, "x", base)
	insertHistoryOrFail(t, r, user, "x", base.Add(historyApart))

	entries, err := r.ListDistinctRecent(t.Context(), user, 10)
	if err != nil || len(entries) != 1 || !entries[0].ExecutedAt.Equal(base.Add(historyApart)) {
		t.Errorf("recent = %v, %v, want the later send", entries, err)
	}
}

func contractHistoryTrimKeepsNewest(t *testing.T, r HistoryRepoUnderTest) {
	user, base := r.NewUser(t), historyBase()
	for i, q := range []string{"a", "b", "c"} {
		insertHistoryOrFail(t, r, user, q, base.Add(time.Duration(i)*2*time.Minute))
	}

	if err := r.TrimToN(t.Context(), user, 2); err != nil {
		t.Fatalf("TrimToN: %v", err)
	}

	if got, want := recentQueries(t, r, user, 10), []string{"c", "b"}; !slices.Equal(got, want) {
		t.Errorf("recent = %v, want %v", got, want)
	}
}

func contractHistoryIsUserScoped(t *testing.T, r HistoryRepoUnderTest) {
	alice, bob := r.NewUser(t), r.NewUser(t)
	insertHistoryOrFail(t, r, alice, "mine", historyBase())

	if got := recentQueries(t, r, bob, 10); len(got) != 0 {
		t.Errorf("bob sees %v, want nothing", got)
	}
	if err := r.TrimToN(t.Context(), bob, 0); err != nil {
		t.Fatalf("TrimToN: %v", err)
	}
	if got := recentQueries(t, r, alice, 10); len(got) != 1 {
		t.Errorf("alice has %v after bob's trim, want one entry", got)
	}
}

func contractHistoryEraseEmpties(t *testing.T, r HistoryRepoUnderTest) {
	user := r.NewUser(t)
	insertHistoryOrFail(t, r, user, "secret", historyBase())

	if err := r.EraseSearchTextForUser(t.Context(), user); err != nil {
		t.Fatalf("EraseSearchTextForUser: %v", err)
	}

	if got := recentQueries(t, r, user, 10); len(got) != 0 {
		t.Errorf("recent = %v after an erase, want nothing", got)
	}
}

func contractHistoryRefusesAStaleInsert(t *testing.T, r HistoryRepoUnderTest) {
	user := r.NewUser(t)
	if err := r.EraseSearchTextForUser(t.Context(), user); err != nil {
		t.Fatalf("EraseSearchTextForUser: %v", err)
	}

	insertHistoryOrFail(t, r, user, "started-before", time.Now().Add(-time.Hour))

	if got := recentQueries(t, r, user, 10); len(got) != 0 {
		t.Errorf("recent = %v, want the pre-erase search dropped", got)
	}
}

func contractHistoryRecordsAfterErase(t *testing.T, r HistoryRepoUnderTest) {
	user := r.NewUser(t)
	if err := r.EraseSearchTextForUser(t.Context(), user); err != nil {
		t.Fatalf("EraseSearchTextForUser: %v", err)
	}

	insertHistoryOrFail(t, r, user, "started-after", time.Now().Add(time.Hour))

	if got, want := recentQueries(t, r, user, 10), []string{"started-after"}; !slices.Equal(got, want) {
		t.Errorf("recent = %v, want %v", got, want)
	}
}

func contractHistoryEraseIsUserScoped(t *testing.T, r HistoryRepoUnderTest) {
	alice, bob := r.NewUser(t), r.NewUser(t)
	insertHistoryOrFail(t, r, alice, "keep", historyBase())
	insertHistoryOrFail(t, r, bob, "wipe", historyBase())

	if err := r.EraseSearchTextForUser(t.Context(), bob); err != nil {
		t.Fatalf("EraseSearchTextForUser: %v", err)
	}

	if got, want := recentQueries(t, r, alice, 10), []string{"keep"}; !slices.Equal(got, want) {
		t.Errorf("alice's recent = %v, want %v", got, want)
	}
}
