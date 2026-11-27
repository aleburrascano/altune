package idfloor

import (
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
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

func (f *Floor) ReserveAbove(ctx context.Context, want uint64) uint64 {
	if want < f.reserved.Load() {
		return want
	}
	return f.reserveBlockFrom(ctx, want)
}

func (f *Floor) reserveBlockFrom(ctx context.Context, want uint64) uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if want < f.reserved.Load() {
		return want
	}
	start := max(want, f.trustedCeiling(ctx, want))
	ceiling := start + idReservation
	f.persistOrWarn(ctx, ceiling)
	f.reserved.Store(ceiling)
	return start
}

func (f *Floor) trustedCeiling(ctx context.Context, want uint64) uint64 {
	ceiling := f.readCeiling(ctx)
	if !isImplausiblyAhead(ceiling, want) {
		return ceiling
	}
	slog.WarnContext(ctx, "events.id_floor_implausible", logging.KeyPath, f.store.Location(), "ceiling", ceiling, "want", want)
	return 0
}

func (f *Floor) readCeiling(ctx context.Context) uint64 {
	raw, err := f.store.Load()
	if errors.Is(err, ErrNotFound) {
		return 0
	}
	if err != nil {
		slog.WarnContext(ctx, "events.id_floor_unreadable", logging.KeyPath, f.store.Location(), logging.KeyError, err)
		return 0
	}
	ceiling, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		slog.WarnContext(ctx, "events.id_floor_corrupt", logging.KeyPath, f.store.Location(), logging.KeyError, err)
		return 0
	}
	return ceiling
}

func isImplausiblyAhead(ceiling, want uint64) bool {
	return ceiling > want && ceiling-want > maxFloorAhead
}

func (f *Floor) persistOrWarn(ctx context.Context, ceiling uint64) {
	if err := f.persist(ceiling); err != nil {
		slog.WarnContext(ctx, "events.id_floor_unwritable", logging.KeyPath, f.store.Location(), logging.KeyError, err)
	}
}

func (f *Floor) persist(ceiling uint64) error {
	if err := f.store.Save([]byte(strconv.FormatUint(ceiling, 10))); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	return nil
}
