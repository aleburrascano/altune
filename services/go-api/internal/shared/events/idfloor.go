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
	idReservation = uint64(time.Second)

	maxFloorAhead = uint64(10 * 365 * 24 * time.Hour)

	idFloorFileName = "altune-event-id-floor"
)

type idFloor struct {
	path     string
	mu       sync.Mutex
	reserved atomic.Uint64
}

func newIDFloor(path string) *idFloor { return &idFloor{path: path} }

func defaultIDFloorPath() string { return filepath.Join(os.TempDir(), idFloorFileName) }

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

func (f *idFloor) trustedCeiling(want uint64) uint64 {
	ceiling := f.readCeiling()
	if !isImplausiblyAhead(ceiling, want) {
		return ceiling
	}
	slog.Warn("events.id_floor_implausible", "path", f.path, "ceiling", ceiling, "want", want)
	return 0
}

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

func isImplausiblyAhead(ceiling, want uint64) bool {
	return ceiling > want && ceiling-want > maxFloorAhead
}

func (f *idFloor) persistOrWarn(ceiling uint64) {
	if err := f.persist(ceiling); err != nil {
		slog.Warn("events.id_floor_unwritable", "path", f.path, "error", err)
	}
}

func (f *idFloor) persist(ceiling uint64) error {
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.FormatUint(ceiling, 10)), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}
