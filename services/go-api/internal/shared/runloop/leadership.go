package runloop

import "context"

// LeadershipScope scopes one pass to the caller's current leadership term: ok
// is false when this instance must not evaluate at all, and the context it
// returns is canceled the moment the term ends, so an evaluation already in
// flight is cut off rather than outliving the term. release ends the pass.
type LeadershipScope func(parent context.Context) (ctx context.Context, release context.CancelFunc, ok bool)

// EveryPassLeads is the default scope: without an election behind it this
// process is the only one running, so every pass proceeds, under a plain
// child of the caller's context.
func EveryPassLeads(parent context.Context) (context.Context, context.CancelFunc, bool) {
	ctx, cancel := context.WithCancel(parent)
	return ctx, cancel, true
}
