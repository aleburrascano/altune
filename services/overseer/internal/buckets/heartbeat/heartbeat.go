package heartbeat

import (
	"altune/overseer/internal/core"
	"context"
	"fmt"
	"time"
)

const capacity = 60

const (
	bucketID      = "heartbeat"
	seriesTickGap = "tick_gap_ms"
)

type Bucket struct {
	store    *core.RingStore
	now      func() time.Time
	series   core.Series
	lastTick time.Time
	haveLast bool
}

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

func (b *Bucket) Rings() map[string]*core.RingStore {
	return map[string]*core.RingStore{"ticks": b.store}
}

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

type Data struct {
	Ticks []core.Signal `json:"ticks"`
}

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
