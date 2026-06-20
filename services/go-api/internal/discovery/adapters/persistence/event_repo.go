package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	"github.com/jackc/pgx/v5/pgxpool"
)

var _ ports.EventStore = (*PgxEventStore)(nil)

type PgxEventStore struct {
	pool *pgxpool.Pool
}

func NewPgxEventStore(pool *pgxpool.Pool) *PgxEventStore {
	return &PgxEventStore{pool: pool}
}

func (r *PgxEventStore) Append(ctx context.Context, event domain.InteractionEvent) error {
	payload := event.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal telemetry payload: %w", err)
	}

	// query_norm is nullable — only search-originated events carry one.
	var queryNorm *string
	if event.QueryNorm != "" {
		queryNorm = &event.QueryNorm
	}

	occurredAt := event.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO discovery_events (user_id, event_type, query_norm, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5)`,
		event.UserId.UUID(), event.Type.String(), queryNorm, string(payloadJSON), occurredAt,
	)
	if err != nil {
		return fmt.Errorf("append telemetry event: %w", err)
	}
	return nil
}
