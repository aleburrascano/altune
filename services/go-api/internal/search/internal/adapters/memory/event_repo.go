package memory

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"cmp"
	"context"
	"slices"
	"sync"
	"time"
)

var _ ports.EventStore = (*EventRepo)(nil)

type EventRepo struct {
	mu     sync.Mutex
	events []domain.InteractionEvent
}

func (r *EventRepo) Append(_ context.Context, event domain.InteractionEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if event.EventId != "" && r.hasEventLocked(event.UserId, event.EventId) {
		return nil
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = clock.Now()
	}
	if event.Type != domain.InteractionTypeSearchPerformed {
		event.QueryNorm = r.searchQueryLocked(event.UserId, event.SearchId)
	}
	r.events = append(r.events, event)
	return nil
}

func (r *EventRepo) NoResultSearches(_ context.Context, since time.Time, limit int) ([]ports.QueryCount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	counts := map[string]int{}
	for _, e := range r.events {
		if r.isSearchSince(e, since) && zeroResult(e) == zeroTrue {
			counts[e.QueryNorm]++
		}
	}
	return rankedCounts(counts, limit), nil
}

func (r *EventRepo) NonZeroNoClickQueries(_ context.Context, since time.Time, limit int) ([]ports.QueryCount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	counts := map[string]int{}
	for _, e := range r.events {
		if r.isSearchSince(e, since) && zeroResult(e) == zeroFalse && !r.clickedSince(e.QueryNorm, since) {
			counts[e.QueryNorm]++
		}
	}
	return rankedCounts(counts, limit), nil
}

type zeroResultState int

const (
	zeroAbsent zeroResultState = iota
	zeroTrue
	zeroFalse
)

func zeroResult(e domain.InteractionEvent) zeroResultState {
	flag, ok := e.Payload[domain.PayloadKeyZeroResult].(bool)
	switch {
	case !ok:
		return zeroAbsent
	case flag:
		return zeroTrue
	default:
		return zeroFalse
	}
}

func (r *EventRepo) isSearchSince(e domain.InteractionEvent, since time.Time) bool {
	return e.Type == domain.InteractionTypeSearchPerformed && e.QueryNorm != "" && !e.OccurredAt.Before(since)
}

func (r *EventRepo) clickedSince(queryNorm string, since time.Time) bool {
	for _, click := range r.events {
		if click.Type != domain.InteractionTypeResultClicked || click.OccurredAt.Before(since) || click.SearchId == "" {
			continue
		}
		if r.searchQueryLocked(click.UserId, click.SearchId) == queryNorm {
			return true
		}
	}
	return false
}

func (r *EventRepo) hasEventLocked(userId ids.UserId, eventId string) bool {
	return slices.ContainsFunc(r.events, func(e domain.InteractionEvent) bool {
		return e.UserId == userId && e.EventId == eventId
	})
}

func (r *EventRepo) searchQueryLocked(userId ids.UserId, searchId string) string {
	if searchId == "" {
		return ""
	}
	for _, e := range r.events {
		if e.UserId == userId && e.SearchId == searchId && e.Type == domain.InteractionTypeSearchPerformed {
			return e.QueryNorm
		}
	}
	return ""
}

func rankedCounts(counts map[string]int, limit int) []ports.QueryCount {
	out := make([]ports.QueryCount, 0, len(counts))
	for norm, n := range counts {
		out = append(out, ports.QueryCount{QueryNorm: norm, Count: n})
	}
	slices.SortFunc(out, func(a, b ports.QueryCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.QueryNorm, b.QueryNorm))
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
