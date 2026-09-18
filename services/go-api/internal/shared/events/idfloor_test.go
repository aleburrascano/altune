package events

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func testFloorPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), idFloorFileName)
}

func writeFloorFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("seeding floor file: %v", err)
	}
}

func TestReserveAbove_WithNoFloorFile_StartsAtTheWallClock(t *testing.T) {
	const wallClock = uint64(1_700_000_000_000_000_000)

	got := newIDFloor(testFloorPath(t)).reserveAbove(wallClock)

	if got != wallClock {
		t.Fatalf("reserveAbove = %d, want the wall clock %d when nothing was persisted", got, wallClock)
	}
}

func TestReserveAbove_WithAFloorAboveTheWallClock_StartsAtTheFloor(t *testing.T) {
	path := testFloorPath(t)
	const persisted = uint64(1_700_000_000_000_000_000)
	writeFloorFile(t, path, strconv.FormatUint(persisted, 10))

	got := newIDFloor(path).reserveAbove(persisted - uint64(time.Hour))

	if got != persisted {
		t.Fatalf("reserveAbove = %d, want the persisted ceiling %d after a backward clock jump", got, persisted)
	}
}

func TestReserveAbove_WithinTheReservedBlock_DoesNotRewriteTheFile(t *testing.T) {
	path := testFloorPath(t)
	floor := newIDFloor(path)
	start := floor.reserveAbove(1_700_000_000_000_000_000)
	reserved := readFloorFile(t, path)

	floor.reserveAbove(start + 1)

	if got := readFloorFile(t, path); got != reserved {
		t.Fatalf("ceiling = %d after an id inside the reserved block, want it untouched at %d", got, reserved)
	}
}

func TestReserveAbove_PastTheReservedBlock_RaisesTheCeiling(t *testing.T) {
	path := testFloorPath(t)
	floor := newIDFloor(path)
	start := floor.reserveAbove(1_700_000_000_000_000_000)
	outgrown := start + idReservation + 1

	floor.reserveAbove(outgrown)

	if got := readFloorFile(t, path); got <= outgrown {
		t.Fatalf("ceiling = %d after issuing %d, want a ceiling above every issued id", got, outgrown)
	}
}

func TestReserveAbove_WithAnUnusableFloorFile_FallsBackToTheWallClock(t *testing.T) {
	const wallClock = uint64(1_700_000_000_000_000_000)
	unusable := map[string]string{
		"corrupt":           "not-a-number",
		"empty":             "",
		"negative":          "-5",
		"implausibly ahead": strconv.FormatUint(wallClock+maxFloorAhead+1, 10),
	}

	for name, contents := range unusable {
		t.Run(name, func(t *testing.T) {
			path := testFloorPath(t)
			writeFloorFile(t, path, contents)

			got := newIDFloor(path).reserveAbove(wallClock)

			if got != wallClock {
				t.Fatalf("reserveAbove = %d, want the wall clock %d for an unusable floor file", got, wallClock)
			}
		})
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
