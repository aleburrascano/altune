package memory

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"context"
	"slices"
	"strings"
	"sync"
	"time"
)

var (
	_ ports.HistoryWriter = (*HistoryRepo)(nil)
	_ ports.HistoryReader = (*HistoryRepo)(nil)
	_ ports.HistoryEraser = (*HistoryRepo)(nil)
)

const historyResendWindow = time.Minute

type HistoryRepo struct {
	mu      sync.Mutex
	entries []domain.SearchHistoryEntry
	cleared map[ids.UserId]time.Time
}

func (r *HistoryRepo) Insert(_ context.Context, entry *domain.SearchHistoryEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if clearedAt, ok := r.cleared[entry.UserId]; ok && clearedAt.After(entry.ExecutedAt) {
		return nil
	}
	if latest, ok := r.latestLocked(entry.UserId); ok &&
		latest.QueryNorm == entry.QueryNorm && latest.ExecutedAt.After(entry.ExecutedAt.Add(-historyResendWindow)) {
		return nil
	}
	r.entries = append(r.entries, *entry)
	return nil
}

func (r *HistoryRepo) TrimToN(_ context.Context, userId ids.UserId, n int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	mine := r.newestFirstLocked(userId)
	keep := map[ids.EntityId]bool{}
	for _, e := range mine[:min(n, len(mine))] {
		keep[e.ID] = true
	}
	r.entries = slices.DeleteFunc(r.entries, func(e domain.SearchHistoryEntry) bool {
		return e.UserId == userId && !keep[e.ID]
	})
	return nil
}

func (r *HistoryRepo) ListDistinctRecent(_ context.Context, userId ids.UserId, limit int) ([]*domain.SearchHistoryEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[string]bool{}
	var out []*domain.SearchHistoryEntry
	for _, e := range r.newestFirstLocked(userId) {
		if seen[e.QueryNorm] {
			continue
		}
		if len(seen) == limit {
			break
		}
		seen[e.QueryNorm] = true
		out = append(out, &e)
	}
	return out, nil
}

func (r *HistoryRepo) EraseSearchTextForUser(_ context.Context, userId ids.UserId) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cleared == nil {
		r.cleared = map[ids.UserId]time.Time{}
	}
	r.cleared[userId] = clock.Now()
	r.entries = slices.DeleteFunc(r.entries, func(e domain.SearchHistoryEntry) bool { return e.UserId == userId })
	return nil
}

func (r *HistoryRepo) latestLocked(userId ids.UserId) (domain.SearchHistoryEntry, bool) {
	mine := r.newestFirstLocked(userId)
	if len(mine) == 0 {
		return domain.SearchHistoryEntry{}, false
	}
	return mine[0], true
}

func (r *HistoryRepo) newestFirstLocked(userId ids.UserId) []domain.SearchHistoryEntry {
	var mine []domain.SearchHistoryEntry
	for _, e := range r.entries {
		if e.UserId == userId {
			mine = append(mine, e)
		}
	}
	slices.SortFunc(mine, func(a, b domain.SearchHistoryEntry) int {
		if c := b.ExecutedAt.Compare(a.ExecutedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID.String(), a.ID.String())
	})
	return mine
}
