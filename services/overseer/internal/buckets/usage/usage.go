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
	"fmt"
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

// Start launches the SSE pump once, bound to the app-lifetime ctx the shell hands
// it — cancelled only at shutdown, so the pump survives the per-tick collect
// deadline (#1812) that froze it after one run when it was launched from Collect.
// The sync.Once makes a second Start a no-op, so the bucket owns exactly one pump
// goroutine however the shell drives it, and that goroutine exits when ctx is
// cancelled at shutdown so nothing leaks.
func (b *Bucket) Start(ctx context.Context) {
	b.start.Do(func() { go b.runSource(ctx) })
}

// Collect drains whatever events have arrived since the last cycle; the SSE pump
// that feeds them runs on the app-lifetime Start hook, not here. An unreachable
// source with nothing fresh returns errSourceDown so the shell keeps the
// last-known rollups and Render flags them stale.
func (b *Bucket) Collect(context.Context) ([]core.Signal, error) {
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

// Data is the usage panel payload: the three bounded rollups, each an ordered
// list of label→count pairs. Labels (search queries, play kinds, window labels)
// are watched-app data carried raw; React escapes them on render.
type Data struct {
	Searches []Count `json:"searches"`
	Plays    []Count `json:"plays"`
	Timeline []Count `json:"timeline"`
	// DroppedKeys is how many distinct search/play keys the bounded rollups have
	// evicted to stay under their cardinality caps. A flood of one-off queries
	// silently drops the lowest-count key; this makes that truncation visible.
	DroppedKeys int `json:"droppedKeys"`
}

// Count is one label→count pair in a usage rollup.
type Count struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// Snapshot builds the usage envelope from the bounded rollups. State follows the
// SSE consumer's status: an unreachable go-api is source_down while the last-known
// rollups are still served, so the panel never goes dark.
//
// Severity is always ok: the rollups describe what the owner DID, and no amount of
// searching or playing is a fault. Grading it would need a threshold nobody owns,
// so the bucket reports its activity as the headline and grades nothing.
func (b *Bucket) Snapshot() core.Snapshot {
	v := b.roll.snapshot()
	searches, plays := counts(v.searches), counts(v.plays)
	return core.Snapshot{
		ID:        b.Meta().ID,
		Title:     b.Meta().Title,
		State:     core.State(b.src.Status().PanelState()),
		Reason:    goapi.StreamReason(b.src),
		Severity:  core.SeverityOK,
		Headline:  usageHeadline(searches, plays),
		UpdatedAt: time.Now().UTC(),
		Data: core.MarshalData(Data{
			Searches:    searches,
			Plays:       plays,
			Timeline:    counts(v.timeline),
			DroppedKeys: v.droppedKeys,
		}),
	}
}

// usageHeadline is the activity the owner would glance at: what was searched and
// what was played over the bounded rollup window.
func usageHeadline(searches, plays []Count) string {
	searched, played := totalCount(searches), totalCount(plays)
	if searched+played == 0 {
		return "no usage recorded yet"
	}
	return fmt.Sprintf("%d searches · %d plays", searched, played)
}

// totalCount sums a rollup's counts. The rollups are bounded top-N lists, so the
// sum is over a fixed small set.
func totalCount(rows []Count) int {
	total := 0
	for _, row := range rows {
		total += row.Count
	}
	return total
}

// counts maps the internal rollup entries onto the exported, JSON-tagged payload.
func counts(entries []entry) []Count {
	out := make([]Count, 0, len(entries))
	for _, e := range entries {
		out = append(out, Count{Label: e.Key, Count: e.Count})
	}
	return out
}

// toSignal renders one go-api event into the shared signal shape. Kind carries the
// event type (used to classify search vs play); Text carries the watched-app
// subject (a search query), HTML-escaped only at render time. A missing OR
// future-dated timestamp is stamped now: the timeline advances monotonically and
// folds older events into the current window, so a single out-of-spec future
// timestamp (clock skew, an NTP jump, a poisoned event) would otherwise ratchet
// the current window into the future and freeze the timeline until wall-clock time
// caught up. Anchoring to the observer's clock keeps the activity timeline honest.
func toSignal(ev goapi.Event) core.Signal {
	at := ev.Timestamp
	now := time.Now()
	if at.IsZero() || at.After(now) {
		at = now
	}
	return core.Signal{At: at, Kind: ev.Type, Text: ev.Subject}
}

// sourceFromEnv builds the SSE consumer from OVERSEER_GOAPI_URL and the
// process-wide operator token source. Missing or invalid config yields a null
// source so the bucket degrades to "source down" instead of failing the whole
// service at startup. This opens the bucket's OWN consumer — its own connection,
// shared with no other bucket (the Usage-opens-its-own-consumer invariant) — but
// takes its credential from the shared goapi.SharedTokenSource so refresh
// single-flights across every bucket.
func sourceFromEnv() source {
	base := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_URL"))
	if base == "" {
		return newNullSource()
	}
	c, err := goapi.NewConsumer(base, goapi.SharedTokenSource())
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

func (n *nullSource) LastError() error {
	return &goapi.SourceDownError{Op: "stream", Err: errSourceDown}
}

func init() { core.Register(New()) }
