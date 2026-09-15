// Package usage is the Overseer Usage bucket: it makes what the owner actually
// does in the app — searches, plays, activity over time — visible from bounded
// rollups. It opens its OWN go-api SSE consumer (its own connection, shared with
// no other bucket) and aggregates every event on ingest into bounded rollups:
// top-N searches, per-kind play counts, and a fixed-window activity timeline. It
// never retains raw unbounded events. When go-api is unreachable the consumer
// reports source-down and the bucket serves its last-known rollups flagged STALE,
// so the shell never goes dark. It owns all its own files and self-registers with
// one blank import in the composition root (the additive-buckets invariant).
package usage

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// errSourceDown is returned by Collect when the event source is unreachable and
// nothing fresh arrived, so the shell logs it and skips the store while Render
// keeps serving last-known rollups flagged stale.
var errSourceDown = errors.New("usage: go-api event source unreachable")

// source is the seam onto the SSE consumer: the subset of *goapi.Consumer the
// bucket needs. Depending on the interface, not the concrete consumer, lets a
// test inject a controllable source and drop it deterministically.
type source interface {
	Run(ctx context.Context) error
	Events() <-chan goapi.Event
	Status() goapi.Status
}

// Bucket ingests go-api domain events into bounded usage rollups and renders them
// as a panel: top searches, per-kind play counts, and an activity timeline.
type Bucket struct {
	roll  *aggregator
	src   source
	start sync.Once
}

// New builds the Usage bucket from the environment. When go-api is not configured
// (no URL or token) it falls back to a null source: the bucket still registers,
// stays bounded, and renders "source down" rather than crashing.
func New() *Bucket { return newBucket(sourceFromEnv()) }

// newBucket is the injectable constructor tests use to supply a controllable
// source; production goes through New.
func newBucket(src source) *Bucket {
	return &Bucket{roll: newAggregator(), src: src}
}

func (b *Bucket) Meta() core.Meta {
	return core.Meta{ID: "usage", Title: "Usage"}
}

// Collect starts the SSE pump once (bound to the app-lifetime ctx), then drains
// whatever events have arrived since the last cycle. An unreachable source with
// nothing fresh returns errSourceDown so the shell keeps the last-known rollups
// and Render flags them stale.
func (b *Bucket) Collect(ctx context.Context) ([]core.Signal, error) {
	b.start.Do(func() { go b.runSource(ctx) })
	signals := b.drain()
	if len(signals) == 0 && b.src.Status() == goapi.StatusDown {
		return nil, errSourceDown
	}
	return signals, nil
}

// runSource runs the SSE pump in its own goroutine, containing any panic so a
// misbehaving source cannot crash the whole process (the degrade-don't-crash
// invariant on the bucket's background path).
func (b *Bucket) runSource(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, "usage: source pump panicked", "recover", rec)
		}
	}()
	_ = b.src.Run(ctx)
}

// drain pulls every buffered event without blocking, converting each to a signal.
// It reads only what is already queued so a cycle never waits on the network.
func (b *Bucket) drain() []core.Signal {
	var signals []core.Signal
	for {
		select {
		case ev, ok := <-b.src.Events():
			if !ok {
				return signals
			}
			signals = append(signals, toSignal(ev))
		default:
			return signals
		}
	}
}

// Store folds each collected signal into the bounded rollups. Nothing raw is
// retained: aggregation happens here, on ingest.
func (b *Bucket) Store(signals []core.Signal) {
	for _, s := range signals {
		b.roll.ingest(s)
	}
}

func (b *Bucket) Render() core.Panel {
	stale := b.src.Status() == goapi.StatusDown
	return core.Panel{Title: b.Meta().Title, Body: renderBody(b.roll.snapshot(), stale)}
}

// toSignal renders one go-api event into the shared signal shape. Kind carries the
// event type (used to classify search vs play); Text carries the watched-app
// subject (a search query), HTML-escaped only at render time. A missing timestamp
// is stamped now so the activity timeline always advances.
func toSignal(ev goapi.Event) core.Signal {
	at := ev.Timestamp
	if at.IsZero() {
		at = time.Now()
	}
	return core.Signal{At: at, Kind: ev.Type, Text: ev.Subject}
}

// sourceFromEnv builds the SSE consumer from OVERSEER_GOAPI_URL and
// OVERSEER_GOAPI_TOKEN. Missing or invalid config yields a null source so the
// bucket degrades to "source down" instead of failing the whole service at
// startup. This opens the bucket's OWN consumer — its own connection, shared with
// no other bucket (the Usage-opens-its-own-consumer invariant).
func sourceFromEnv() source {
	base := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_URL"))
	token := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_TOKEN"))
	if base == "" || token == "" {
		return newNullSource()
	}
	c, err := goapi.NewConsumer(base, goapi.StaticTokenSource(token))
	if err != nil {
		// Degrade to source-down, but say why: without this a URL typo is
		// indistinguishable from go-api being genuinely down (a permanently-STALE
		// panel with no diagnostic).
		slog.Warn("usage: invalid OVERSEER_GOAPI_URL, degrading to source-down", "error", err)
		return newNullSource()
	}
	return c
}

// nullSource stands in when go-api is unconfigured: it is permanently down and
// never delivers an event, so an unconfigured bucket renders stale and bounded
// rather than nil-panicking on Status/Events.
type nullSource struct{ events chan goapi.Event }

func newNullSource() *nullSource { return &nullSource{events: make(chan goapi.Event)} }

func (n *nullSource) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (n *nullSource) Events() <-chan goapi.Event    { return n.events }
func (n *nullSource) Status() goapi.Status          { return goapi.StatusDown }

func init() { core.Register(New()) }
