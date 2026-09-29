package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"time"
)

const discographyRetentionWindow = 400 * 24 * time.Hour

const pruneEventsByTypeSQL = `DELETE FROM discovery_events
	WHERE event_type = $1 AND occurred_at < $2`

const aggregateEventRetention = 90 * 24 * time.Hour

const AggregateEventRetention = aggregateEventRetention

const writeOnlyEventRetention = 30 * 24 * time.Hour

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
	{domain.EventTypeUserAction, writeOnlyEventRetention},
	{domain.EventTypeFailureShown, writeOnlyEventRetention},
}

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
