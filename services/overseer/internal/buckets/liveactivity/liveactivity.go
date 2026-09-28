package liveactivity

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

const eventCapacity = 200

var errSourceDown = errors.New("liveactivity: go-api event source unreachable")

type source interface {
	Run(ctx context.Context) error
	Events() <-chan goapi.Event
	Status() goapi.Status
}

type Bucket struct {
	events core.Store
	src    source
	start  sync.Once
}

func New() *Bucket { return newBucket(sourceFromEnv()) }

func newBucket(src source) *Bucket {
	return &Bucket{events: core.NewRingStore(eventCapacity), src: src}
}

func (b *Bucket) Meta() core.Meta {
	return core.Meta{ID: "liveactivity", Title: "Live activity"}
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
			slog.ErrorContext(ctx, "liveactivity: source pump panicked", "recover", rec)
		}
	}()
	_ = b.src.Run(ctx)
}

func (b *Bucket) drain() []core.Signal {
	var signals []core.Signal
	for _, ev := range goapi.DrainPending(b.src, b.src.Events()) {
		signals = append(signals, toSignal(ev))
	}
	return signals
}

func (b *Bucket) Store(signals []core.Signal) {
	for _, s := range signals {
		b.events.Add(s)
	}
}

type Data struct {
	Events            []core.Signal `json:"events"`
	InFlight          int           `json:"inFlight"`
	InFlightAvailable bool          `json:"inFlightAvailable"`
	Dropped           int           `json:"dropped"`
}

func (b *Bucket) Snapshot() core.Snapshot {
	events := b.events.Snapshot()
	updated := time.Time{}
	if n := len(events); n > 0 {
		updated = events[n-1].At
	}
	status, failure := goapi.StreamStatus(b.src)
	return core.Snapshot{
		ID:        b.Meta().ID,
		Title:     b.Meta().Title,
		State:     core.State(status.PanelState()),
		Reason:    status.PanelReason(failure),
		Severity:  core.SeverityOK,
		Headline:  eventsHeadline(len(events)),
		UpdatedAt: updated,
		Data:      core.MarshalData(Data{Events: events, InFlight: 0, InFlightAvailable: false, Dropped: goapi.TotalDropped(b.src, b.events.Dropped())}),
	}
}

func eventsHeadline(events int) string {
	if events == 0 {
		return "no events yet"
	}
	return fmt.Sprintf("%d events", events)
}

func toSignal(ev goapi.Event) core.Signal {
	return core.Signal{At: ev.Timestamp, Kind: ev.Type, Text: eventText(ev), CorrID: ev.CorrID}
}

func eventText(ev goapi.Event) string {
	parts := []string{ev.Type}
	if ev.User != "" {
		parts = append(parts, "user="+ev.User)
	}
	if ev.Subject != "" {
		parts = append(parts, ev.Subject)
	}
	return strings.Join(parts, " ")
}

func sourceFromEnv() source {
	base := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_URL"))
	if base == "" {
		return newNullSource()
	}
	c, err := goapi.NewConsumer(base, goapi.SharedTokenSource())
	if err != nil {
		slog.Warn("liveactivity: invalid OVERSEER_GOAPI_URL, degrading to source-down", "error", err)
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

func init() { core.Register(New()) }
