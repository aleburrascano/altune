package events

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// idReservation is how many IDs a process claims per write to the floor
	// file. Sizing it only trades ID space against write frequency: a uint64
	// holds billions of blocks this size, and a process that outruns one
	// reserves again.
	idReservation = uint64(time.Second)

	// maxFloorAhead bounds how far ahead of the wall clock a floor file may
	// push IDs. A corrupt or tampered file would otherwise strand every
	// future ID near the top of the uint64 range with no recovery short of
	// deleting the file by hand.
	maxFloorAhead = uint64(10 * 365 * 24 * time.Hour)

	idFloorFileName = "altune-event-id-floor"
)

// idFloor is the event-ID high-water mark kept outside the process, because Go's
// monotonic clock reading does not survive a restart. Without it a wall clock
// that steps backward between restarts (NTP correction, VM or container skew)
// seeds the new process below IDs a reconnecting client already holds, and that
// client's afterID filter discards every event the new process publishes.
//
// Reading the file is best effort: an unreadable or implausible floor degrades
// to the wall clock alone, since refusing to serve events is worse than the gap
// it protects against. One process owns one floor file.
type idFloor struct {
	path     string
	mu       sync.Mutex
	reserved atomic.Uint64
}

func newIDFloor(path string) *idFloor { return &idFloor{path: path} }

// defaultIDFloorPath keeps the floor on local disk, which survives a process
// restart on the same host but not a fresh container. A host with no floor to
// read falls back to the wall clock, the behaviour this type replaces.
func defaultIDFloorPath() string { return filepath.Join(os.TempDir(), idFloorFileName) }

// reserveAbove returns the first ID safe to issue at or above want, having
// persisted a ceiling above it first. The result exceeds want when an earlier
// process reserved higher, which is the whole point: want comes from a wall
// clock that can have moved backward since. A caller that is only keeping the
// reservation ahead of an ID it already issued may ignore the result.
func (f *idFloor) reserveAbove(want uint64) uint64 {
	if want < f.reserved.Load() {
		return want
	}
	return f.reserveBlockFrom(want)
}

func (f *idFloor) reserveBlockFrom(want uint64) uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if want < f.reserved.Load() {
		return want
	}
	start := max(want, f.trustedCeiling(want))
	ceiling := start + idReservation
	f.persistOrWarn(ceiling)
	f.reserved.Store(ceiling)
	return start
}

// trustedCeiling is the ceiling a previous process reserved, or 0 when there is
// none worth trusting. want anchors the plausibility bound, so a tampered file
// cannot push IDs arbitrarily far into the future.
func (f *idFloor) trustedCeiling(want uint64) uint64 {
	ceiling := f.readCeiling()
	if !isImplausiblyAhead(ceiling, want) {
		return ceiling
	}
	slog.Warn("events.id_floor_implausible", "path", f.path, "ceiling", ceiling, "want", want)
	return 0
}

// readCeiling returns 0 for a floor file that is absent, unreadable, or not a
// uint64, all of which mean the same thing to the caller: nothing to resume from.
func (f *idFloor) readCeiling() uint64 {
	raw, err := os.ReadFile(f.path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0
	}
	if err != nil {
		slog.Warn("events.id_floor_unreadable", "path", f.path, "error", err)
		return 0
	}
	ceiling, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		slog.Warn("events.id_floor_corrupt", "path", f.path, "error", err)
		return 0
	}
	return ceiling
}

// isImplausiblyAhead subtracts rather than adding maxFloorAhead to want so a
// ceiling near the top of the uint64 range cannot wrap the comparison around.
func isImplausiblyAhead(ceiling, want uint64) bool {
	return ceiling > want && ceiling-want > maxFloorAhead
}

// persistOrWarn keeps an unwritable floor from taking the process down: the
// cost is a floor the next restart cannot read, which is where this started,
// and the operator gets told.
func (f *idFloor) persistOrWarn(ceiling uint64) {
	if err := f.persist(ceiling); err != nil {
		slog.Warn("events.id_floor_unwritable", "path", f.path, "error", err)
	}
}

// persist writes through a temp file and a rename so a crash mid-write leaves
// the previous ceiling intact rather than a truncated number that would read
// back as a lower floor.
func (f *idFloor) persist(ceiling uint64) error {
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.FormatUint(ceiling, 10)), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}
