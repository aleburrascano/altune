package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

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

func (s *FallbackCooldownStore) Reserve(ctx context.Context, trackID domain.TrackId, kind ports.CooldownKind, cooldown time.Duration) (time.Time, bool, error) {
	if s.mem.active(trackID, kind, cooldown) {
		return time.Time{}, false, nil
	}
	at, ok, err := s.primary.Reserve(ctx, trackID, kind, cooldown)
	if isUndefinedTable(err) {
		s.markDegraded(ctx, err)
		at, ok = s.mem.reserve(trackID, kind, cooldown)
		return at, ok, nil
	}
	if err == nil {
		s.markRecovered(ctx)
	}
	return at, ok, err
}

func (s *FallbackCooldownStore) Release(ctx context.Context, trackID domain.TrackId, kind ports.CooldownKind, at time.Time) error {
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
	track domain.TrackId
	kind  ports.CooldownKind
}

func newMemoryCooldowns(now func() time.Time) *memoryCooldowns {
	return &memoryCooldowns{now: now, lastAt: make(map[memoryCooldownKey]time.Time)}
}

func (m *memoryCooldowns) active(trackID domain.TrackId, kind ports.CooldownKind, cooldown time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	last, ok := m.lastAt[memoryCooldownKey{trackID, kind}]
	return ok && m.now().Sub(last) < cooldown
}

func (m *memoryCooldowns) reserve(trackID domain.TrackId, kind ports.CooldownKind, cooldown time.Duration) (time.Time, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now, key := m.now(), memoryCooldownKey{trackID, kind}
	if last, ok := m.lastAt[key]; ok && now.Sub(last) < cooldown {
		return time.Time{}, false
	}
	m.lastAt[key] = now
	for k, v := range m.lastAt {
		if now.Sub(v) >= 2*cooldown {
			delete(m.lastAt, k)
		}
	}
	return now, true
}

func (m *memoryCooldowns) release(trackID domain.TrackId, kind ports.CooldownKind, at time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := memoryCooldownKey{trackID, kind}
	if last, ok := m.lastAt[key]; ok && last.Equal(at) {
		delete(m.lastAt, key)
		return true
	}
	return false
}
