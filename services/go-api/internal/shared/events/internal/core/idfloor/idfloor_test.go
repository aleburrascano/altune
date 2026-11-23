package idfloor_test

import (
	"altune/go-api/internal/shared/events/internal/core/idfloor"
	"context"
	"log/slog"
	"os"
	"path/filepath"
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

func testFloorPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "altune-event-id-floor")
}

func writeFloorFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("seeding floor file: %v", err)
	}
}

func readFloorFile(t *testing.T, path string) uint64 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading floor file: %v", err)
	}
	ceiling, err := strconv.ParseUint(string(raw), 10, 64)
	if err != nil {
		t.Fatalf("floor file holds %q, want a uint64: %v", raw, err)
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

func TestReserveAbove_WithNoFloorFile_StartsAtTheWallClockAndPersistsAReservation(t *testing.T) {
	path := testFloorPath(t)

	got := idfloor.New(path).ReserveAbove(wallClock)

	if got != wallClock {
		t.Fatalf("ReserveAbove = %d, want the wall clock %d when nothing was persisted", got, wallClock)
	}
	if ceiling := readFloorFile(t, path); ceiling != wallClock+reservation {
		t.Fatalf("ceiling = %d, want %d: one second of ids reserved above the start", ceiling, wallClock+reservation)
	}
}

func TestReserveAbove_WithAFloorAboveTheWallClock_StartsAtTheFloor(t *testing.T) {
	path := testFloorPath(t)
	const persisted = wallClock
	writeFloorFile(t, path, strconv.FormatUint(persisted, 10))

	got := idfloor.New(path).ReserveAbove(persisted - uint64(time.Hour))

	if got != persisted {
		t.Fatalf("ReserveAbove = %d, want the persisted ceiling %d after a backward clock jump", got, persisted)
	}
}

func TestReserveAbove_TrustsAFloorExactlyTenYearsAhead(t *testing.T) {
	path := testFloorPath(t)
	edge := wallClock + maxFloorAhead
	writeFloorFile(t, path, strconv.FormatUint(edge, 10))

	if got := idfloor.New(path).ReserveAbove(wallClock); got != edge {
		t.Fatalf("ReserveAbove = %d, want the floor %d: only a floor more than ten years ahead is implausible", got, edge)
	}
}

func TestReserveAbove_WithinTheReservedBlock_DoesNotRewriteTheFile(t *testing.T) {
	path := testFloorPath(t)
	floor := idfloor.New(path)
	start := floor.ReserveAbove(wallClock)
	reserved := readFloorFile(t, path)

	if got := floor.ReserveAbove(start + 1); got != start+1 {
		t.Fatalf("ReserveAbove = %d, want the asked-for id %d inside the reserved block", got, start+1)
	}

	if got := readFloorFile(t, path); got != reserved {
		t.Fatalf("ceiling = %d after an id inside the reserved block, want it untouched at %d", got, reserved)
	}
}

func TestReserveAbove_AtTheReservedCeiling_RaisesTheCeiling(t *testing.T) {
	path := testFloorPath(t)
	floor := idfloor.New(path)
	start := floor.ReserveAbove(wallClock)
	ceiling := start + reservation

	floor.ReserveAbove(ceiling)

	if got := readFloorFile(t, path); got <= ceiling {
		t.Fatalf("ceiling = %d after issuing %d, want a ceiling above every issued id", got, ceiling)
	}
}

func TestReserveAbove_PastTheReservedBlock_RaisesTheCeiling(t *testing.T) {
	path := testFloorPath(t)
	floor := idfloor.New(path)
	start := floor.ReserveAbove(wallClock)
	outgrown := start + reservation + 1

	floor.ReserveAbove(outgrown)

	if got := readFloorFile(t, path); got <= outgrown {
		t.Fatalf("ceiling = %d after issuing %d, want a ceiling above every issued id", got, outgrown)
	}
}

func TestReserveAbove_WithAnUnusableFloorFile_FallsBackToTheWallClock(t *testing.T) {
	unusable := map[string]struct{ contents, warning string }{
		"corrupt":           {"not-a-number", "events.id_floor_corrupt"},
		"empty":             {"", "events.id_floor_corrupt"},
		"negative":          {"-5", "events.id_floor_corrupt"},
		"implausibly ahead": {strconv.FormatUint(wallClock+maxFloorAhead+1, 10), "events.id_floor_implausible"},
	}

	for name, tc := range unusable {
		t.Run(name, func(t *testing.T) {
			warnings := captureWarnings(t)
			path := testFloorPath(t)
			writeFloorFile(t, path, tc.contents)

			got := idfloor.New(path).ReserveAbove(wallClock)

			if got != wallClock {
				t.Fatalf("ReserveAbove = %d, want the wall clock %d for an unusable floor file", got, wallClock)
			}
			warnings.only(t, tc.warning)
		})
	}
}

func TestReserveAbove_WithAnUnreadableFloorPath_FallsBackToTheWallClockAndWarns(t *testing.T) {
	warnings := captureWarnings(t)
	path := t.TempDir()

	if got := idfloor.New(path).ReserveAbove(wallClock); got != wallClock {
		t.Fatalf("ReserveAbove = %d, want the wall clock %d", got, wallClock)
	}
	warnings.mu.Lock()
	defer warnings.mu.Unlock()
	if len(warnings.messages) != 2 || warnings.messages[0] != "events.id_floor_unreadable" || warnings.messages[1] != "events.id_floor_unwritable" {
		t.Fatalf("warnings = %q, want [events.id_floor_unreadable events.id_floor_unwritable]: a directory can be neither read nor replaced", warnings.messages)
	}
}

func TestReserveAbove_WhenTheFloorCannotBeWritten_StillIssuesAndWarns(t *testing.T) {
	warnings := captureWarnings(t)
	path := filepath.Join(t.TempDir(), "missing-dir", "altune-event-id-floor")

	if got := idfloor.New(path).ReserveAbove(wallClock); got != wallClock {
		t.Fatalf("ReserveAbove = %d, want the wall clock %d", got, wallClock)
	}
	warnings.only(t, "events.id_floor_unwritable")
}
