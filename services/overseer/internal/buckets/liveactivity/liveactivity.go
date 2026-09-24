// Package liveactivity is the first real Overseer bucket: it makes the watched
// app feel alive at a glance. It consumes go-api's operator event stream (via the
// goapi SSE Consumer) into a bounded ring and renders a live feed, alongside an
// in-flight-requests signal. When go-api is unreachable the consumer reports
// source-down and the bucket serves its last-known feed flagged STALE, so the
// shell never goes dark — the outlives-the-app invariant made real at a leaf. It
// owns all its own files and self-registers with one blank import in the
// composition root (the additive-buckets invariant).
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

// eventCapacity bounds the retained live-event feed. The ring caps memory by
// construction no matter how long the stream runs or how fast events arrive.
const eventCapacity = 200

// errSourceDown is returned by Collect when the event source is unreachable and
// nothing fresh arrived, so the shell logs it and skips the store while Render
// keeps serving last-known state flagged stale (the outlives-the-app degrade
// path, exactly as the Bucket contract prescribes).
var errSourceDown = errors.New("liveactivity: go-api event source unreachable")

// source is the seam onto the SSE consumer: the subset of *goapi.Consumer the
// bucket needs. Depending on the interface, not the concrete consumer, lets a
// test inject a controllable source and drop it deterministically.
type source interface {
	Run(ctx context.Context) error
	Events() <-chan goapi.Event
	Status() goapi.Status
}

// Bucket ingests go-api domain events into a bounded ring and renders them as a
// live feed plus an in-flight-requests signal.
type Bucket struct {
	events core.Store
	src    source
	start  sync.Once
}

// New builds the Live activity bucket from the environment. When go-api is not
// configured (no URL or token) it falls back to a null source: the bucket still
// registers, stays bounded and renders "source down" rather than crashing.
func New() *Bucket { return newBucket(sourceFromEnv()) }

// newBucket is the injectable constructor tests use to supply a controllable
// source; production goes through New.
func newBucket(src source) *Bucket {
	return &Bucket{events: core.NewRingStore(eventCapacity), src: src}
}

func (b *Bucket) Meta() core.Meta {
	return core.Meta{ID: "liveactivity", Title: "Live activity"}
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
// last-known feed and Render flags it stale.
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
			slog.ErrorContext(ctx, "liveactivity: source pump panicked", "recover", rec)
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

func (b *Bucket) Store(signals []core.Signal) {
	for _, s := range signals {
		b.events.Add(s)
	}
}

// Data is the live-activity panel payload the bespoke frontend panel renders: the
// bounded event feed (newest last), and the in-flight-requests signal. Every event
// Text is watched-app data, carried raw — React escapes it on render, which is the
// escaping invariant moved from html/template to the client.
type Data struct {
	Events []core.Signal `json:"events"`
	// InFlight is the in-flight-requests count. go-api exposes no in-flight read
	// yet (the read-only client has only /health, and the event stream carries
	// point-in-time domain events, not request start/end pairs), so this stays 0
	// with InFlightAvailable=false — an explicit, visible missing-coverage marker
	// the panel surfaces rather than a fabricated number.
	InFlight          int  `json:"inFlight"`
	InFlightAvailable bool `json:"inFlightAvailable"`
	// Dropped is how many older events the ring has evicted under a burst. The
	// feed renders only the retained window; this makes the truncation visible so
	// an operator can tell a full window from a lossy one during an incident.
	Dropped int `json:"dropped"`
}

// Snapshot builds the live-activity envelope. State follows the SSE consumer's
// connection status: up is live, a drop/reconnect is stale, and an unreachable
// go-api is source_down — the bucket keeps serving the last-known feed either way,
// so the panel never goes dark. UpdatedAt is the newest event's timestamp.
//
// Severity is always ok: a domain-event feed carries no failure — go-api emits
// what happened, not what went wrong — and the in-flight gauge that could degrade
// has no read behind it yet (InFlightAvailable=false). Grading event volume would
// need a threshold nobody owns, so the bucket reports its depth and grades nothing.
func (b *Bucket) Snapshot() core.Snapshot {
	events := b.events.Snapshot()
	updated := time.Time{}
	if n := len(events); n > 0 {
		updated = events[n-1].At
	}
	status := b.src.Status()
	return core.Snapshot{
		ID:        b.Meta().ID,
		Title:     b.Meta().Title,
		State:     core.State(status.PanelState()),
		Reason:    status.PanelReason(goapi.StreamReason(b.src)),
		Severity:  core.SeverityOK,
		Headline:  eventsHeadline(len(events)),
		UpdatedAt: updated,
		Data:      core.MarshalData(Data{Events: events, InFlight: 0, InFlightAvailable: false, Dropped: b.events.Dropped()}),
	}
}

// eventsHeadline is the depth of the retained feed — the one figure the panel
// leads with.
func eventsHeadline(events int) string {
	if events == 0 {
		return "no events yet"
	}
	return fmt.Sprintf("%d events", events)
}

// toSignal renders one go-api event into the shared signal shape. The text is
// built from watched-app data and stored raw; it is HTML-escaped at render time.
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

// sourceFromEnv builds the SSE consumer from OVERSEER_GOAPI_URL and the
// process-wide operator token source. Missing or invalid config yields a null
// source so the bucket degrades to "source down" instead of failing the whole
// service at startup. Config normally lives in the config package, which this
// leaf may not edit; reading the go-api URL here and taking the credential from
// goapi.SharedTokenSource keeps the change within the bucket.
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
		slog.Warn("liveactivity: invalid OVERSEER_GOAPI_URL, degrading to source-down", "error", err)
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
