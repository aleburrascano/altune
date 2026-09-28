package usage

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	bucketID             = "usage"
	seriesRequestsPerMin = "requests_per_min"
	seriesActiveUsers    = "active_users"
)

var errSourceDown = errors.New("usage: go-api event source unreachable")

type source interface {
	Run(ctx context.Context) error
	Events() <-chan goapi.Event
	Status() goapi.Status
}

type Bucket struct {
	roll   *aggregator
	src    source
	start  sync.Once
	series core.Series
	window *activityWindow
}

func New() *Bucket { return newBucket(sourceFromEnv()) }

func newBucket(src source) *Bucket {
	return &Bucket{
		roll:   newAggregator(),
		src:    src,
		series: discardSeries{},
		window: newActivityWindow(timelineWindow),
	}
}

func (b *Bucket) Meta() core.Meta {
	return core.Meta{ID: bucketID, Title: "Usage"}
}

func (b *Bucket) UseSeries(s core.Series) {
	b.series = s
}

func (b *Bucket) KeySeries() string {
	return seriesRequestsPerMin
}

func (b *Bucket) Start(ctx context.Context) {
	b.start.Do(func() { go b.runSource(ctx) })
}

func (b *Bucket) Collect(context.Context) ([]core.Signal, error) {
	signals := b.drain()
	if len(signals) == 0 && b.src.Status() == goapi.StatusDown {
		return nil, errSourceDown
	}
	return signals, nil
}

func (b *Bucket) runSource(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, "usage: source pump panicked", "recover", rec)
		}
	}()
	_ = b.src.Run(ctx)
}

func (b *Bucket) drain() []core.Signal {
	var signals []core.Signal
	for _, ev := range goapi.DrainPending(b.src, b.src.Events()) {
		sig := toSignal(ev)
		signals = append(signals, sig)
		b.recordWindow(sig.At, ev.User)
	}
	return signals
}

func (b *Bucket) recordWindow(at time.Time, user string) {
	totals, ok := b.window.add(at, user)
	if !ok {
		return
	}
	b.series.Record(bucketID, seriesRequestsPerMin, core.Point{At: totals.at, Value: float64(totals.requests)})
	b.series.Record(bucketID, seriesActiveUsers, core.Point{At: totals.at, Value: float64(totals.users)})
}

func (b *Bucket) Store(signals []core.Signal) {
	for _, s := range signals {
		b.roll.ingest(s)
	}
}

type Data struct {
	Searches    []Count `json:"searches"`
	Plays       []Count `json:"plays"`
	Timeline    []Count `json:"timeline"`
	DroppedKeys int     `json:"droppedKeys"`
	Dropped     int     `json:"dropped"`
}

type Count struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

func (b *Bucket) Snapshot() core.Snapshot {
	v := b.roll.snapshot()
	searches, plays := counts(v.searches), counts(v.plays)
	status, failure := goapi.StreamStatus(b.src)
	return core.Snapshot{
		ID:        b.Meta().ID,
		Title:     b.Meta().Title,
		State:     core.State(status.PanelState()),
		Reason:    status.PanelReason(failure),
		Severity:  core.SeverityOK,
		Headline:  usageHeadline(searches, plays),
		UpdatedAt: time.Now().UTC(),
		Data: core.MarshalData(Data{
			Searches:    searches,
			Plays:       plays,
			Timeline:    counts(v.timeline),
			DroppedKeys: v.droppedKeys,
			Dropped:     goapi.TotalDropped(b.src, 0),
		}),
	}
}

func usageHeadline(searches, plays []Count) string {
	searched, played := totalCount(searches), totalCount(plays)
	if searched+played == 0 {
		return "no usage recorded yet"
	}
	return fmt.Sprintf("%d searches · %d plays", searched, played)
}

func totalCount(rows []Count) int {
	total := 0
	for _, row := range rows {
		total += row.Count
	}
	return total
}

func counts(entries []entry) []Count {
	out := make([]Count, 0, len(entries))
	for _, e := range entries {
		out = append(out, Count{Label: e.Key, Count: e.Count})
	}
	return out
}

func toSignal(ev goapi.Event) core.Signal {
	at := ev.Timestamp
	now := time.Now()
	if at.IsZero() || at.After(now) {
		at = now
	}
	return core.Signal{At: at, Kind: ev.Type, Text: ev.Subject}
}

func sourceFromEnv() source {
	base := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_URL"))
	if base == "" {
		return newNullSource()
	}
	c, err := goapi.NewConsumer(base, goapi.SharedTokenSource())
	if err != nil {
		slog.Warn("usage: invalid OVERSEER_GOAPI_URL, degrading to source-down", "error", err)
		return newNullSource()
	}
	return c
}

type nullSource struct{ events chan goapi.Event }

func newNullSource() *nullSource { return &nullSource{events: make(chan goapi.Event)} }

func (n *nullSource) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (n *nullSource) Events() <-chan goapi.Event    { return n.events }
func (n *nullSource) Status() goapi.Status          { return goapi.StatusDown }

func (n *nullSource) LastError() error {
	return &goapi.SourceDownError{Op: "stream", Err: errSourceDown}
}

type discardSeries struct{}

func (discardSeries) Record(string, string, core.Point) {}

func (discardSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, nil
}

func init() { core.Register(New()) }
