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

// undefinedTableCode is the Postgres SQLSTATE for a missing relation.
const undefinedTableCode = "42P01"

// cooldownMigration names the migration whose absence triggers the fallback.
const cooldownMigration = "migrations/019_acquisition_cooldowns.sql"

// FallbackCooldownStore wraps the durable cooldown store and degrades to a
// per-process cooldown while the acquisition_cooldowns table does not exist.
// Deploys ship before migrations are applied by hand, so without it every
// retry/reacquire would fail with 500 until 019 runs. The primary store is
// tried on every call, so the durable path resumes as soon as the table
// appears, with no restart. Any other primary error is returned unchanged.
type FallbackCooldownStore struct {
	primary  ports.CooldownStore
	mem      *memoryCooldowns
	degraded atomic.Bool
}

var _ ports.CooldownStore = (*FallbackCooldownStore)(nil)

func NewFallbackCooldownStore(primary ports.CooldownStore) *FallbackCooldownStore {
	return &FallbackCooldownStore{primary: primary, mem: newMemoryCooldowns(time.Now)}
}

// Reserve refuses while a per-process reservation from the degraded period is
// still inside its window, so the switch back to the durable store cannot
// admit a track twice in one window. Otherwise it reserves in the primary
// store, falling back to the process when the table is missing.
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

// Release refunds a per-process reservation when at matches one; otherwise the
// reservation came from the primary store and is released there. A missing
// table there means nothing durable exists to release.
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

// markDegraded logs one warning per transition into the fallback, not one per
// request.
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

// memoryCooldowns is the per-process cooldown used before #986: one admission
// per (track, kind) per window, with release undoing exactly one reservation.
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

// reserve atomically checks the window and records now as the last admission,
// pruning entries old enough that no window can still cover them.
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

// release deletes the reservation recorded at at and reports whether it did.
// A newer reservation is left untouched.
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
