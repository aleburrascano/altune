package core

import (
	"context"
	"html/template"
)

// Meta identifies a bucket to the shell and registry. ID must be unique and
// stable; Title is the human label on the rendered panel.
type Meta struct {
	ID    string
	Title string
}

// Panel is a bucket's contribution to the shell UI: a title plus a fragment of
// pre-escaped HTML. Buckets build Body with html/template so the shell can embed
// it without re-escaping.
type Panel struct {
	Title string
	Body  template.HTML
}

// Bucket is the plugin contract. The shell core drives every bucket through the
// same three-step cycle and never references a concrete implementation:
//
//	Collect — gather fresh signals from the watched app.
//	Store   — persist them through a bounded Store.
//	Render  — contribute a panel built from the stored signals.
//
// A bucket owns its own files and self-registers with exactly one registration
// line, which is the "additive buckets" invariant made real.
type Bucket interface {
	Meta() Meta
	// Collect gathers signals; an unreachable source returns an error and the
	// shell keeps serving the bucket's last-known state.
	Collect(ctx context.Context) ([]Signal, error)
	// Store persists the given signals through the bucket's bounded Store.
	Store(signals []Signal)
	// Render builds the bucket's panel from its currently stored signals.
	Render() Panel
}
