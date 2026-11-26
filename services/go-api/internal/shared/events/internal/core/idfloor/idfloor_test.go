package idfloor_test

import (
	"altune/go-api/internal/shared/events/internal/core/idfloor"
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"
)

const (
	wallClock     = uint64(1_700_000_000_000_000_000)
	reservation   = uint64(time.Second)
	maxFloorAhead = uint64(10 * 365 * 24 * time.Hour)
)

type memoryStore struct {
	stored  *string
	loadErr error
	saveErr error
	saves   int
}

func storeHolding(contents string) *memoryStore {
	return &memoryStore{stored: &contents}
}

func (m *memoryStore) Location() string { return "memory" }

func (m *memoryStore) Load() ([]byte, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	if m.stored == nil {
		return nil, idfloor.ErrNotFound
	}
	return []byte(*m.stored), nil
}

func (m *memoryStore) Save(raw []byte) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saves++
	contents := string(raw)
	m.stored = &contents
	return nil
}

func (m *memoryStore) ceiling(t *testing.T) uint64 {
	t.Helper()
	if m.stored == nil {
		t.Fatal("nothing was saved")
	}
	ceiling, err := strconv.ParseUint(*m.stored, 10, 64)
	if err != nil {
		t.Fatalf("store holds %q, want a uint64: %v", *m.stored, err)
	}
	return ceiling
}

type warnRecorder struct {
	mu       sync.Mutex
	messages []string
}

func (h *warnRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (h *warnRecorder) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r.Level == slog.LevelWarn {
		h.messages = append(h.messages, r.Message)
	}
	return nil
}

func (h *warnRecorder) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *warnRecorder) WithGroup(string) slog.Handler { return h }

func captureWarnings(t *testing.T) *warnRecorder {
	t.Helper()
	rec := &warnRecorder{}
	prev := slog.Default()
	slog.SetDefault(slog.New(rec))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return rec
}

func (h *warnRecorder) only(t *testing.T, want string) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.messages) != 1 || h.messages[0] != want {
		t.Fatalf("warnings = %q, want exactly [%s]", h.messages, want)
	}
}

func TestReserveAbove_WithNothingStored_StartsAtTheWallClockAndPersistsAReservation(t *testing.T) {
	store := &memoryStore{}

	got := idfloor.New(store).ReserveAbove(context.Background(), wallClock)

	if got != wallClock {
		t.Fatalf("ReserveAbove = %d, want the wall clock %d when nothing was persisted", got, wallClock)
	}
	if ceiling := store.ceiling(t); ceiling != wallClock+reservation {
		t.Fatalf("ceiling = %d, want %d: one second of ids reserved above the start", ceiling, wallClock+reservation)
	}
}

func TestReserveAbove_WithAFloorAboveTheWallClock_StartsAtTheFloor(t *testing.T) {
	const persisted = wallClock
	store := storeHolding(strconv.FormatUint(persisted, 10))

	got := idfloor.New(store).ReserveAbove(context.Background(), persisted-uint64(time.Hour))

	if got != persisted {
		t.Fatalf("ReserveAbove = %d, want the persisted ceiling %d after a backward clock jump", got, persisted)
	}
}

func TestReserveAbove_TrustsAFloorExactlyTenYearsAhead(t *testing.T) {
	edge := wallClock + maxFloorAhead
	store := storeHolding(strconv.FormatUint(edge, 10))

	if got := idfloor.New(store).ReserveAbove(context.Background(), wallClock); got != edge {
		t.Fatalf("ReserveAbove = %d, want the floor %d: only a floor more than ten years ahead is implausible", got, edge)
	}
}

func TestReserveAbove_WithinTheReservedBlock_DoesNotRewriteTheStore(t *testing.T) {
	store := &memoryStore{}
	floor := idfloor.New(store)
	start := floor.ReserveAbove(context.Background(), wallClock)
	reserved := store.ceiling(t)

	if got := floor.ReserveAbove(context.Background(), start+1); got != start+1 {
		t.Fatalf("ReserveAbove = %d, want the asked-for id %d inside the reserved block", got, start+1)
	}

	if got := store.ceiling(t); got != reserved || store.saves != 1 {
		t.Fatalf("ceiling = %d after %d saves, want it untouched at %d after one", got, store.saves, reserved)
	}
}

func TestReserveAbove_AtTheReservedCeiling_RaisesTheCeiling(t *testing.T) {
	store := &memoryStore{}
	floor := idfloor.New(store)
	start := floor.ReserveAbove(context.Background(), wallClock)
	ceiling := start + reservation

	floor.ReserveAbove(context.Background(), ceiling)

	if got := store.ceiling(t); got <= ceiling {
		t.Fatalf("ceiling = %d after issuing %d, want a ceiling above every issued id", got, ceiling)
	}
}

func TestReserveAbove_PastTheReservedBlock_RaisesTheCeiling(t *testing.T) {
	store := &memoryStore{}
	floor := idfloor.New(store)
	start := floor.ReserveAbove(context.Background(), wallClock)
	outgrown := start + reservation + 1

	floor.ReserveAbove(context.Background(), outgrown)

	if got := store.ceiling(t); got <= outgrown {
		t.Fatalf("ceiling = %d after issuing %d, want a ceiling above every issued id", got, outgrown)
	}
}

func TestReserveAbove_WithAnUnusableStoredFloor_FallsBackToTheWallClock(t *testing.T) {
	unusable := map[string]struct{ contents, warning string }{
		"corrupt":           {"not-a-number", "events.id_floor_corrupt"},
		"empty":             {"", "events.id_floor_corrupt"},
		"negative":          {"-5", "events.id_floor_corrupt"},
		"implausibly ahead": {strconv.FormatUint(wallClock+maxFloorAhead+1, 10), "events.id_floor_implausible"},
	}

	for name, tc := range unusable {
		t.Run(name, func(t *testing.T) {
			warnings := captureWarnings(t)

			got := idfloor.New(storeHolding(tc.contents)).ReserveAbove(context.Background(), wallClock)

			if got != wallClock {
				t.Fatalf("ReserveAbove = %d, want the wall clock %d for an unusable floor", got, wallClock)
			}
			warnings.only(t, tc.warning)
		})
	}
}

func TestReserveAbove_WithAnUnreadableStore_FallsBackToTheWallClockAndWarns(t *testing.T) {
	warnings := captureWarnings(t)
	store := &memoryStore{loadErr: errors.New("permission denied"), saveErr: errors.New("read-only")}

	if got := idfloor.New(store).ReserveAbove(context.Background(), wallClock); got != wallClock {
		t.Fatalf("ReserveAbove = %d, want the wall clock %d", got, wallClock)
	}
	warnings.mu.Lock()
	defer warnings.mu.Unlock()
	if len(warnings.messages) != 2 || warnings.messages[0] != "events.id_floor_unreadable" || warnings.messages[1] != "events.id_floor_unwritable" {
		t.Fatalf("warnings = %q, want [events.id_floor_unreadable events.id_floor_unwritable]", warnings.messages)
	}
}

func TestReserveAbove_WhenTheFloorCannotBeSaved_StillIssuesAndWarns(t *testing.T) {
	warnings := captureWarnings(t)
	store := &memoryStore{saveErr: errors.New("disk full")}

	if got := idfloor.New(store).ReserveAbove(context.Background(), wallClock); got != wallClock {
		t.Fatalf("ReserveAbove = %d, want the wall clock %d", got, wallClock)
	}
	warnings.only(t, "events.id_floor_unwritable")
}
