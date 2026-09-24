// Package heartbeat is the tracer-bullet bucket: a trivial tick that proves the
// whole plugin path end to end — register -> collect -> bounded store -> render
// -> served. It owns all its own files and self-registers with one line (the
// blank import in the composition root).
package heartbeat

import (
	"altune/overseer/internal/core"
	"context"
	"fmt"
	"time"
)

// capacity bounds the retained heartbeat ticks. The ring enforces the
// bounded-storage invariant regardless of how long the service runs.
const capacity = 60

const (
	bucketID      = "heartbeat"
	seriesTickGap = "tick_gap_ms"
)

// Bucket collects a periodic tick and renders the recent ones.
type Bucket struct {
	store    core.Store
	now      func() time.Time
	series   core.Series
	lastTick time.Time
	haveLast bool
}

// New builds a heartbeat bucket backed by a bounded ring store.
func New() *Bucket {
	return &Bucket{store: core.NewRingStore(capacity), now: time.Now, series: discardSeries{}}
}

func (b *Bucket) Meta() core.Meta {
	return core.Meta{ID: bucketID, Title: "Heartbeat"}
}

func (b *Bucket) UseSeries(s core.Series) {
	b.series = s
}

func (b *Bucket) KeySeries() string {
	return seriesTickGap
}

// Collect emits a single tick signal. It never fails: the heartbeat's source is
// Overseer's own clock, which proves the collect path without depending on the
// watched app being up.
func (b *Bucket) Collect(_ context.Context) ([]core.Signal, error) {
	now := b.now()
	b.recordGap(now)
	return []core.Signal{{
		At:   now,
		Kind: "tick",
		Text: "alive at " + now.UTC().Format(time.RFC3339),
	}}, nil
}

func (b *Bucket) recordGap(now time.Time) {
	if b.haveLast {
		gap := now.Sub(b.lastTick)
		b.series.Record(bucketID, seriesTickGap, core.Point{At: now.UTC(), Value: float64(gap.Microseconds()) / 1000})
	}
	b.lastTick = now
	b.haveLast = true
}

func (b *Bucket) Store(signals []core.Signal) {
	for _, s := range signals {
		b.store.Add(s)
	}
}

// Data is the heartbeat panel payload: the recent ticks, newest last. Text is
// Overseer's own clock, not watched-app data, but the frontend escapes it like
// everything else on render.
type Data struct {
	Ticks []core.Signal `json:"ticks"`
}

// Snapshot builds the heartbeat envelope. The source is Overseer's own clock, so
// the state is always live: the heartbeat proves the collect path without
// depending on the watched app being up. UpdatedAt is the newest stored tick.
//
// Severity is always ok: a tick from Overseer's own clock cannot report a fault,
// and a stalled collect loop shows as a frozen UpdatedAt rather than a grade — the
// bucket has no cadence to compare against, so it invents no threshold.
func (b *Bucket) Snapshot() core.Snapshot {
	ticks := b.store.Snapshot()
	updated := time.Time{}
	if n := len(ticks); n > 0 {
		updated = ticks[n-1].At
	}
	return core.Snapshot{
		ID:        b.Meta().ID,
		Title:     b.Meta().Title,
		State:     core.StateLive,
		Severity:  core.SeverityOK,
		Headline:  ticksHeadline(len(ticks)),
		UpdatedAt: updated,
		Data:      core.MarshalData(Data{Ticks: ticks}),
	}
}

// ticksHeadline is the depth of the retained tick ring: how much of the collect
// path this bucket has proven so far.
func ticksHeadline(ticks int) string {
	if ticks == 0 {
		return "no ticks yet"
	}
	return fmt.Sprintf("%d ticks", ticks)
}

type discardSeries struct{}

func (discardSeries) Record(string, string, core.Point) {}

func (discardSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, nil
}

func init() { core.Register(New()) }
