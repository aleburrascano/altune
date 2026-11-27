package persistence

import (
	"altune/go-api/internal/platform/monitoring/internal/core/domain"
	"altune/go-api/internal/platform/monitoring/internal/core/ports"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ ports.ClientEventStore = (*PostgresClientEventStore)(nil)

type PostgresClientEventStore struct {
	pool *pgxpool.Pool
}

func NewPostgresClientEventStore(pool *pgxpool.Pool) *PostgresClientEventStore {
	if pool == nil {
		panic("persistence.NewPostgresClientEventStore: a pool is required")
	}
	return &PostgresClientEventStore{pool: pool}
}

const appendClientEventSQL = `INSERT INTO client_events
		(user_id, event_type, event_id, correlation_id, client_occurred_at, payload, occurred_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	ON CONFLICT (user_id, event_id) WHERE event_id IS NOT NULL DO NOTHING`

func (s *PostgresClientEventStore) Append(ctx context.Context, event domain.ClientEvent) error {
	payload := event.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal client event payload: %w", err)
	}

	var eventID *uuid.UUID
	if event.EventId != "" {
		id, parseErr := uuid.Parse(event.EventId)
		if parseErr != nil {
			return fmt.Errorf("parse client event id: %w", parseErr)
		}
		eventID = &id
	}
	var correlationID *string
	if event.CorrelationId != "" {
		correlationID = &event.CorrelationId
	}
	var clientOccurredAt *time.Time
	if !event.ClientOccurredAt.IsZero() {
		t := event.ClientOccurredAt.UTC()
		clientOccurredAt = &t
	}

	if _, err := s.pool.Exec(ctx, appendClientEventSQL,
		event.UserId.UUID(), string(event.Type), eventID, correlationID, clientOccurredAt, string(payloadJSON), event.OccurredAt.UTC(),
	); err != nil {
		return database.WrapUnreadable("append client event", ports.ErrClientEventsUnavailable, err)
	}
	return nil
}

const eraseClientEventsOfDeletedIdentitiesSQL = `
	DELETE FROM client_events e
	WHERE EXISTS (SELECT 1 FROM auth_user_ids)
	  AND e.user_id <> $1
	  AND NOT EXISTS (SELECT 1 FROM auth_user_ids u WHERE u.id = e.user_id)`

func (s *PostgresClientEventStore) EraseRowsOfDeletedIdentities(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, eraseClientEventsOfDeletedIdentitiesSQL, ids.SystemUserId().UUID())
	if err != nil {
		return 0, database.WrapUnreadable("erase client events of deleted identities", ports.ErrClientEventsUnavailable, err)
	}
	return tag.RowsAffected(), nil
}
