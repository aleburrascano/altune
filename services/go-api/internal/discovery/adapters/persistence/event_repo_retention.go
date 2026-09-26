package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"time"
)

const discographyRetentionWindow = 400 * 24 * time.Hour

// pruneEventsByTypeSQL evicts one event type's rows strictly older than the
// cutoff. It is keyed on (event_type, occurred_at), served by
// idx_discovery_events_type_time, so the delete touches only the tail it removes.
// The prune is always keyed on a single type against that type's own cutoff — a
// blanket table-wide age-prune would evict rows of a type read over a wide window
// using another type's narrow one, so retention is enforced strictly per type.
const pruneEventsByTypeSQL = `DELETE FROM discovery_events
	WHERE event_type = $1 AND occurred_at < $2`

// aggregateEventRetention bounds how long the discovery_events types that feed a
// windowed aggregate are kept. The widest window any of them is read over is the
// 30-day behavioral/corpus/coverage lookback (a satisfaction join reaches ~24h
// further back through shownResultWindow); 90 days keeps a ~3x margin over that,
// so the prune can never evict a row a live read could still scan. It also bounds
// the effective history the offline coverage/behavioral eval (-since-days) can
// surface, the same role maxQualityWindowDays plays for discography.
const aggregateEventRetention = 90 * 24 * time.Hour

// AggregateEventRetention exposes aggregateEventRetention to the offline eval CLI
// so it can clamp its -since-days read to the window the prune actually keeps,
// the same role maxQualityWindowDays plays for the discography read path. One
// source of truth for the ceiling: the value that bounds eviction is the value
// that bounds reads.
const AggregateEventRetention = aggregateEventRetention

// writeOnlyEventRetention bounds the discovery_events types no aggregate reads
// (results_shown, search_failed, search_degraded, playback_health,
// detail_health). Their read
// window is zero, so any positive retention is safe; 30 days bounds their growth
// while leaving an operator a month of raw telemetry to inspect.
const writeOnlyEventRetention = 30 * 24 * time.Hour

// eventRetention is the per-type retention policy for every persisted
// discovery_events type except discography_observed, which owns the wider
// discographyRetentionWindow (tied to its 365-day read cap) and its own eviction.
// Each window is strictly wider than the widest window any aggregate reads that
// type over, so pruning a type can never remove a row another type's read — or its
// own — could still serve.
var eventRetention = []struct {
	eventType domain.EventType
	window    time.Duration
}{
	{domain.EventTypeSearchPerformed, aggregateEventRetention},
	{domain.EventTypeResultClicked, aggregateEventRetention},
	{domain.EventTypePlay, aggregateEventRetention},
	{domain.EventTypeSkip, aggregateEventRetention},
	{domain.EventTypeCompleted, aggregateEventRetention},
	{domain.EventTypeLibraryAdd, aggregateEventRetention},
	{domain.EventTypeWrongAlbum, aggregateEventRetention},
	{domain.EventTypeResultsShown, writeOnlyEventRetention},
	{domain.EventTypeSearchFailed, writeOnlyEventRetention},
	{domain.EventTypeSearchDegraded, writeOnlyEventRetention},
	{domain.EventTypePlaybackHealth, writeOnlyEventRetention},
	{domain.EventTypeDetailHealth, writeOnlyEventRetention},
	{domain.EventTypeAcquisitionUi, writeOnlyEventRetention},
	{domain.EventTypeClientError, writeOnlyEventRetention},
}

// PruneEvents evicts every non-discography event type older than that type's own
// retention window measured back from now, returning the total rows removed. Each
// type is deleted against its own cutoff, so a type read over a wide window is
// never evicted by one read over a narrow window. It is idempotent: each run
// re-evaluates the whole tail against the current cutoff, so a missed run defers
// eviction without ever skipping a row. discography_observed is pruned separately
// by PruneDiscographyObserved, which owns its wider window.
func (r *PgxEventStore) PruneEvents(ctx context.Context, now time.Time) (int64, error) {
	var total int64
	for _, ret := range eventRetention {
		cutoff := now.UTC().Add(-ret.window)
		tag, err := r.pool.Exec(ctx, pruneEventsByTypeSQL, ret.eventType.String(), cutoff)
		if err != nil {
			return total, fmt.Errorf("prune %s events: %w", ret.eventType, err)
		}
		total += tag.RowsAffected()
	}
	return total, nil
}

// PruneDiscographyObserved evicts discography_observed events older than the
// retention window measured back from now, returning the rows removed. The cutoff
// (now - discographyRetentionWindow) is always older than the widest readable
// window, so the prune bounds the table's growth on every discography open without
// ever removing a row the aggregate could still serve. It is idempotent: a missed
// run defers eviction but never skips a row, because each run re-evaluates the
// whole tail against the current cutoff rather than a since-last-run slice.
func (r *PgxEventStore) PruneDiscographyObserved(ctx context.Context, now time.Time) (int64, error) {
	cutoff := now.UTC().Add(-discographyRetentionWindow)
	tag, err := r.pool.Exec(ctx, pruneEventsByTypeSQL,
		domain.EventTypeDiscographyObserved.String(), cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("prune discography events: %w", err)
	}
	return tag.RowsAffected(), nil
}
