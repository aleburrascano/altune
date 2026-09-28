package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const dbCallTimeout = 5 * time.Second

type PgxCooldownStore struct {
	pool *pgxpool.Pool
}

var _ ports.CooldownStore = (*PgxCooldownStore)(nil)

func NewPgxCooldownStore(pool *pgxpool.Pool) *PgxCooldownStore {
	return &PgxCooldownStore{pool: pool}
}

const reserveSQL = `
INSERT INTO acquisition_cooldowns (track_id, kind, admitted_at)
VALUES ($1, $2, clock_timestamp())
ON CONFLICT (track_id, kind) DO UPDATE
	SET admitted_at = EXCLUDED.admitted_at
	WHERE acquisition_cooldowns.admitted_at <= EXCLUDED.admitted_at - make_interval(secs => $3)
RETURNING admitted_at`

func (s *PgxCooldownStore) Reserve(ctx context.Context, trackID domain.TrackId, kind ports.CooldownKind, cooldown time.Duration) (time.Time, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	var at time.Time
	err := s.pool.QueryRow(ctx, reserveSQL, trackID.UUID(), string(kind), cooldown.Seconds()).Scan(&at)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("reserve cooldown: %w", err)
	}
	return at, true, nil
}

func (s *PgxCooldownStore) Release(ctx context.Context, trackID domain.TrackId, kind ports.CooldownKind, at time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	_, err := s.pool.Exec(ctx,
		`DELETE FROM acquisition_cooldowns WHERE track_id = $1 AND kind = $2 AND admitted_at = $3`,
		trackID.UUID(), string(kind), at)
	if err != nil {
		return fmt.Errorf("release cooldown: %w", err)
	}
	return nil
}
