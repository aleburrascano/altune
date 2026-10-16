package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
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
WITH up AS (
INSERT INTO acquisition_cooldowns (track_id, kind, admitted_at)
VALUES ($1, $2, clock_timestamp())
ON CONFLICT (track_id, kind) DO UPDATE
	SET admitted_at = EXCLUDED.admitted_at
	WHERE acquisition_cooldowns.admitted_at <= EXCLUDED.admitted_at - make_interval(secs => $3)
RETURNING admitted_at)
SELECT admitted_at, true, 0::float8 FROM up
UNION ALL
SELECT c.admitted_at, false, EXTRACT(EPOCH FROM c.admitted_at + make_interval(secs => $3) - clock_timestamp())::float8
FROM acquisition_cooldowns c WHERE c.track_id = $1 AND c.kind = $2 AND NOT EXISTS (SELECT 1 FROM up)`

func (s *PgxCooldownStore) Reserve(ctx context.Context, trackID shared.TrackId, kind ports.CooldownKind, cooldown time.Duration) (time.Time, bool, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	var (
		at       time.Time
		ok       bool
		waitSecs float64
	)
	err := s.pool.QueryRow(ctx, reserveSQL, trackID.UUID(), string(kind), cooldown.Seconds()).Scan(&at, &ok, &waitSecs)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, cooldown, nil
	}
	if err != nil {
		return time.Time{}, false, 0, fmt.Errorf("reserve cooldown: %w", err)
	}
	if !ok {
		return time.Time{}, false, time.Duration(waitSecs * float64(time.Second)), nil
	}
	return at, true, 0, nil
}

func (s *PgxCooldownStore) Release(ctx context.Context, trackID shared.TrackId, kind ports.CooldownKind, at time.Time) error {
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
