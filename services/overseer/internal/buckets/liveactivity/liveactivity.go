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
	"html/template"
	"log/slog"
	"os"
	"strings"
	"sync"
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

// Collect starts the SSE pump once (bound to the app-lifetime ctx), then drains
// whatever events have arrived since the last cycle. An unreachable source with
// nothing fresh returns errSourceDown so the shell keeps the last-known feed and
// Render flags it stale.
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

func (b *Bucket) Render() core.Panel {
	events := b.events.Snapshot()
	stale := b.src.Status() == goapi.StatusDown
	return core.Panel{Title: b.Meta().Title, Body: renderBody(events, stale)}
}

func renderBody(events []core.Signal, stale bool) template.HTML {
	var sb strings.Builder
	sb.WriteString(statusLine(stale))
	sb.WriteString(inflightLine())
	sb.WriteString(feed(events))
	return template.HTML(sb.String()) //nolint:gosec // every dynamic part escaped in feed()
}

func statusLine(stale bool) string {
	if stale {
		return `<p class="empty">STALE — go-api unreachable, showing last-known activity</p>`
	}
	return `<p>LIVE — streaming go-api events</p>`
}

// inflightLine renders the in-flight-requests signal. go-api exposes no
// in-flight-requests read yet: the read-only client has only /health, and the
// operator event stream carries point-in-time domain events, not request
// start/end pairs. Rather than invent an endpoint, this is an explicit bounded
// "no source yet" gap — a visible missing-coverage marker per the shape, ready to
// fill when go-api grows the read.
func inflightLine() string {
	return `<p class="empty">In flight: 0 (no go-api in-flight read yet)</p>`
}

func feed(events []core.Signal) string {
	if len(events) == 0 {
		return `<p class="empty">no events yet</p>`
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "<p>%d event(s)</p><ul>", len(events))
	for _, s := range events {
		// The text is watched-app data; escape it so a hostile event payload can
		// never inject markup into the trusted panel HTML.
		sb.WriteString("<li>" + template.HTMLEscapeString(s.Text) + "</li>")
	}
	sb.WriteString("</ul>")
	return sb.String()
}

// toSignal renders one go-api event into the shared signal shape. The text is
// built from watched-app data and stored raw; it is HTML-escaped at render time.
func toSignal(ev goapi.Event) core.Signal {
	return core.Signal{At: ev.Timestamp, Kind: ev.Type, Text: eventText(ev)}
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

func init() { core.Register(New()) }
