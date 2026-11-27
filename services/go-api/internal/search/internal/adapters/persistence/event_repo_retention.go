package persistence

import (
	"altune/go-api/internal/search/internal/domain"
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
	eventType domain.InteractionType
	window    time.Duration
}{
	{domain.InteractionTypeSearchPerformed, aggregateEventRetention},
	{domain.InteractionTypeResultClicked, aggregateEventRetention},
	{domain.InteractionTypePlay, aggregateEventRetention},
	{domain.InteractionTypeSkip, aggregateEventRetention},
	{domain.InteractionTypeCompleted, aggregateEventRetention},
	{domain.InteractionTypeLibraryAdd, aggregateEventRetention},
	{domain.InteractionTypeWrongAlbum, aggregateEventRetention},
	{domain.InteractionTypeResultsShown, writeOnlyEventRetention},
	{domain.InteractionTypeSearchFailed, writeOnlyEventRetention},
	{domain.InteractionTypeSearchDegraded, writeOnlyEventRetention},
	{domain.InteractionTypePlaybackHealth, writeOnlyEventRetention},
	{domain.InteractionTypeDetailHealth, writeOnlyEventRetention},
	{domain.InteractionTypeAcquisitionUi, writeOnlyEventRetention},
	{domain.InteractionTypeClientError, writeOnlyEventRetention},
	{domain.InteractionTypeUserAction, writeOnlyEventRetention},
	{domain.InteractionTypeFailureShown, writeOnlyEventRetention},
	{domain.InteractionTypeSseReconnect, writeOnlyEventRetention},
	{domain.InteractionTypeOutboxFlushFailed, writeOnlyEventRetention},
	{domain.InteractionTypeDownloadFailed, writeOnlyEventRetention},
	{domain.InteractionTypeAuthFailed, writeOnlyEventRetention},
	{domain.InteractionTypeScreenView, writeOnlyEventRetention},
	{domain.InteractionTypePlayStart, writeOnlyEventRetention},
}

func (r *PostgresEventStore) PruneEvents(ctx context.Context, now time.Time) (int64, error) {
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

func (r *PostgresEventStore) PruneDiscographyObserved(ctx context.Context, now time.Time) (int64, error) {
	cutoff := now.UTC().Add(-discographyRetentionWindow)
	tag, err := r.pool.Exec(ctx, pruneEventsByTypeSQL,
		domain.InteractionTypeDiscographyObserved.String(), cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("prune discography events: %w", err)
	}
	return tag.RowsAffected(), nil
}
