package idfloor

import (
	"context"
	"errors"
	"log/slog"
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

var ErrNotFound = errors.New("id floor: nothing stored yet")

type Store interface {
	Load() ([]byte, error)
	Save(raw []byte) error
	Location() string
}

type Floor struct {
	store    Store
	mu       sync.Mutex
	reserved atomic.Uint64
}

func New(store Store) *Floor { return &Floor{store: store} }

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
	slog.WarnContext(context.Background(), "events.id_floor_implausible", "path", f.store.Location(), "ceiling", ceiling, "want", want)
	return 0
}

func (f *Floor) readCeiling() uint64 {
	raw, err := f.store.Load()
	if errors.Is(err, ErrNotFound) {
		return 0
	}
	if err != nil {
		slog.WarnContext(context.Background(), "events.id_floor_unreadable", "path", f.store.Location(), "error", err)
		return 0
	}
	ceiling, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		slog.WarnContext(context.Background(), "events.id_floor_corrupt", "path", f.store.Location(), "error", err)
		return 0
	}
	return ceiling
}

func isImplausiblyAhead(ceiling, want uint64) bool {
	return ceiling > want && ceiling-want > maxFloorAhead
}

func (f *Floor) persistOrWarn(ceiling uint64) {
	if err := f.persist(ceiling); err != nil {
		slog.WarnContext(context.Background(), "events.id_floor_unwritable", "path", f.store.Location(), "error", err)
	}
}

func (f *Floor) persist(ceiling uint64) error {
	return f.store.Save([]byte(strconv.FormatUint(ceiling, 10)))
}
