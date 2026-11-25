package idfloor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	idReservation = uint64(time.Second)

	maxFloorAhead = uint64(10 * 365 * 24 * time.Hour)
)

type Floor struct {
	path     string
	mu       sync.Mutex
	reserved atomic.Uint64
}

func New(path string) *Floor { return &Floor{path: path} }

func (f *Floor) ReserveAbove(want uint64) uint64 {
	if want < f.reserved.Load() {
		return want
	}
	return f.reserveBlockFrom(want)
}

func (f *Floor) reserveBlockFrom(want uint64) uint64 {
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

func (f *Floor) trustedCeiling(want uint64) uint64 {
	ceiling := f.readCeiling()
	if !isImplausiblyAhead(ceiling, want) {
		return ceiling
	}
	slog.WarnContext(context.Background(), "events.id_floor_implausible", "path", f.path, "ceiling", ceiling, "want", want)
	return 0
}

func (f *Floor) readCeiling() uint64 {
	raw, err := os.ReadFile(f.path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0
	}
	if err != nil {
		slog.WarnContext(context.Background(), "events.id_floor_unreadable", "path", f.path, "error", err)
		return 0
	}
	ceiling, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		slog.WarnContext(context.Background(), "events.id_floor_corrupt", "path", f.path, "error", err)
		return 0
	}
	return ceiling
}

func isImplausiblyAhead(ceiling, want uint64) bool {
	return ceiling > want && ceiling-want > maxFloorAhead
}

func (f *Floor) persistOrWarn(ceiling uint64) {
	if err := f.persist(ceiling); err != nil {
		slog.WarnContext(context.Background(), "events.id_floor_unwritable", "path", f.path, "error", err)
	}
}

func (f *Floor) persist(ceiling uint64) error {
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.FormatUint(ceiling, 10)), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, f.path); err != nil {
		return fmt.Errorf("replace %s: %w", f.path, err)
	}
	return nil
}
