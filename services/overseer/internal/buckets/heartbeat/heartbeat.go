// Package heartbeat is the tracer-bullet bucket: a trivial tick that proves the
// whole plugin path end to end — register -> collect -> bounded store -> render
// -> served. It owns all its own files and self-registers with one line (the
// blank import in the composition root).
package heartbeat

import (
	"altune/overseer/internal/core"
	"context"
	"fmt"
	"html/template"
	"strings"
	"time"
)

// capacity bounds the retained heartbeat ticks. The ring enforces the
// bounded-storage invariant regardless of how long the service runs.
const capacity = 60

// Bucket collects a periodic tick and renders the recent ones.
type Bucket struct {
	store core.Store
	now   func() time.Time
}

// New builds a heartbeat bucket backed by a bounded ring store.
func New() *Bucket {
	return &Bucket{store: core.NewRingStore(capacity), now: time.Now}
}

func (b *Bucket) Meta() core.Meta {
	return core.Meta{ID: "heartbeat", Title: "Heartbeat"}
}

// Collect emits a single tick signal. It never fails: the heartbeat's source is
// Overseer's own clock, which proves the collect path without depending on the
// watched app being up.
func (b *Bucket) Collect(_ context.Context) ([]core.Signal, error) {
	now := b.now()
	return []core.Signal{{
		At:   now,
		Kind: "tick",
		Text: "alive at " + now.UTC().Format(time.RFC3339),
	}}, nil
}

func (b *Bucket) Store(signals []core.Signal) {
	for _, s := range signals {
		b.store.Add(s)
	}
}

func (b *Bucket) Render() core.Panel {
	signals := b.store.Snapshot()
	return core.Panel{Title: b.Meta().Title, Body: renderBody(signals)}
}

func renderBody(signals []core.Signal) template.HTML {
	if len(signals) == 0 {
		return template.HTML("<p class=\"empty\">no ticks yet</p>") //nolint:gosec // static literal
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "<p>%d tick(s) stored</p><ul>", len(signals))
	for _, s := range signals {
		// template.HTMLEscapeString guards against any future non-static text.
		sb.WriteString("<li>" + template.HTMLEscapeString(s.Text) + "</li>")
	}
	sb.WriteString("</ul>")
	return template.HTML(sb.String()) //nolint:gosec // all dynamic parts escaped above
}

func init() { core.Register(New()) }
