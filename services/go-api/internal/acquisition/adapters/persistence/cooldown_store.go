package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

const undefinedTableCode = "42P01"

const cooldownMigration = "migrations/019_acquisition_cooldowns.sql"

type FallbackCooldownStore struct {
	primary  ports.CooldownStore
	mem      *memoryCooldowns
	degraded atomic.Bool
}

var _ ports.CooldownStore = (*FallbackCooldownStore)(nil)

func NewFallbackCooldownStore(primary ports.CooldownStore) *FallbackCooldownStore {
	return &FallbackCooldownStore{primary: primary, mem: newMemoryCooldowns(time.Now)}
}

func (s *FallbackCooldownStore) Reserve(ctx context.Context, trackID shared.TrackId, kind ports.CooldownKind, cooldown time.Duration) (time.Time, bool, time.Duration, error) {
	if wait, active := s.mem.active(trackID, kind, cooldown); active {
		return time.Time{}, false, wait, nil
	}
	at, ok, wait, err := s.primary.Reserve(ctx, trackID, kind, cooldown)
	if isUndefinedTable(err) {
		s.markDegraded(ctx, err)
		at, ok, wait = s.mem.reserve(trackID, kind, cooldown)
		return at, ok, wait, nil
	}
	if err == nil {
		s.markRecovered(ctx)
	}
	return at, ok, wait, err
}

func (s *FallbackCooldownStore) Release(ctx context.Context, trackID shared.TrackId, kind ports.CooldownKind, at time.Time) error {
	if s.mem.release(trackID, kind, at) {
		return nil
	}
	err := s.primary.Release(ctx, trackID, kind, at)
	if isUndefinedTable(err) {
		return nil
	}
	return err
}

func (s *FallbackCooldownStore) markDegraded(ctx context.Context, err error) {
	if s.degraded.CompareAndSwap(false, true) {
		slog.WarnContext(ctx, "acquisition: cooldown table missing, using per-process cooldown until migration is applied",
			"migration", cooldownMigration, "error", err)
	}
}

func (s *FallbackCooldownStore) markRecovered(ctx context.Context) {
	if s.degraded.CompareAndSwap(true, false) {
		slog.InfoContext(ctx, "acquisition: cooldown table present, durable cooldown resumed", "migration", cooldownMigration)
	}
}

func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == undefinedTableCode
}

type memoryCooldowns struct {
	mu     sync.Mutex
	now    func() time.Time
	lastAt map[memoryCooldownKey]time.Time
}

type memoryCooldownKey struct {
	track shared.TrackId
	kind  ports.CooldownKind
}

func newMemoryCooldowns(now func() time.Time) *memoryCooldowns {
	return &memoryCooldowns{now: now, lastAt: make(map[memoryCooldownKey]time.Time)}
}

func (m *memoryCooldowns) active(trackID shared.TrackId, kind ports.CooldownKind, cooldown time.Duration) (time.Duration, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	last, ok := m.lastAt[memoryCooldownKey{trackID, kind}]
	if !ok {
		return 0, false
	}
	elapsed := m.now().Sub(last)
	return cooldown - elapsed, elapsed < cooldown
}

func (m *memoryCooldowns) reserve(trackID shared.TrackId, kind ports.CooldownKind, cooldown time.Duration) (time.Time, bool, time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now, key := m.now(), memoryCooldownKey{trackID, kind}
	if last, ok := m.lastAt[key]; ok && now.Sub(last) < cooldown {
		return time.Time{}, false, cooldown - now.Sub(last)
	}
	m.lastAt[key] = now
	for k, v := range m.lastAt {
		if now.Sub(v) >= 2*cooldown {
			delete(m.lastAt, k)
		}
	}
	return now, true, 0
}

func (m *memoryCooldowns) release(trackID shared.TrackId, kind ports.CooldownKind, at time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := memoryCooldownKey{trackID, kind}
	if last, ok := m.lastAt[key]; ok && last.Equal(at) {
		delete(m.lastAt, key)
		return true
	}
	return false
}
