// Package stub is a second, deliberately minimal bucket. Its only job is to
// prove the additive-buckets invariant: it registers with one import line in the
// composition root and touches no other bucket's files. When the shape needs a
// real bucket here later, this is replaced, not extended.
package stub

import (
	"altune/overseer/internal/core"
	"context"
	"html/template"
)

// Bucket is a placeholder plugin that renders a static panel.
type Bucket struct {
	store core.Store
}

// New builds the stub bucket over a tiny bounded store.
func New() *Bucket {
	return &Bucket{store: core.NewRingStore(1)}
}

func (b *Bucket) Meta() core.Meta {
	return core.Meta{ID: "stub", Title: "Stub"}
}

// Collect records that the shape holds: a second bucket collects independently.
func (b *Bucket) Collect(_ context.Context) ([]core.Signal, error) {
	return []core.Signal{{Kind: "stub", Text: "registered additively"}}, nil
}

func (b *Bucket) Store(signals []core.Signal) {
	for _, s := range signals {
		b.store.Add(s)
	}
}

func (b *Bucket) Render() core.Panel {
	body := template.HTML("<p class=\"empty\">placeholder bucket</p>") //nolint:gosec // static literal
	return core.Panel{Title: b.Meta().Title, Body: body}
}

func init() { core.Register(New()) }
