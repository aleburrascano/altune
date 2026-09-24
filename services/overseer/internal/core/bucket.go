package core

import (
	"context"
)

// Meta identifies a bucket to the shell and registry. ID must be unique and
// stable; Title is the human label the frontend shows on the panel.
type Meta struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Bucket is the plugin contract. The shell core drives every bucket through the
// same cycle and never references a concrete implementation:
//
//	Collect  — gather fresh signals from the watched app.
//	Store    — persist them through a bounded Store.
//	Snapshot — contribute a JSON-serializable envelope built from stored state.
//
// A bucket owns its own files and self-registers with exactly one registration
// line, which is the "additive buckets" invariant made real. Snapshot replaced
// the old Render() Panel: the core is a pure JSON API now, so no bucket emits
// HTML and core no longer imports html/template.
type Bucket interface {
	Meta() Meta
	// Collect gathers signals; an unreachable source returns an error and the
	// shell keeps serving the bucket's last-known state.
	Collect(ctx context.Context) ([]Signal, error)
	// Store persists the given signals through the bucket's bounded Store.
	Store(signals []Signal)
	// Snapshot builds the bucket's JSON envelope from its currently stored state.
	Snapshot() Snapshot
}

// Starter is the optional lifecycle hook a bucket implements when it owns
// background work — a scheduler or a source pump — that must outlive a single
// collect tick. The shell calls Start exactly once at startup, before the tick
// loop, with the app-lifetime context: cancelled only at shutdown, never by the
// per-bucket collect deadline. Start must return promptly, launching its loop on
// its own goroutine, and that loop must exit when ctx is cancelled so nothing
// leaks past the app's lifetime. Background work launched from Collect's ctx
// instead dies the moment Collect returns, because that ctx carries the collect
// timeout; Start is where such work belongs. A bucket with no background work
// implements no Starter and the shell skips it.
type Starter interface {
	Start(ctx context.Context)
}

type Waiter interface {
	Wait()
}
