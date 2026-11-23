package fanout

import (
	"altune/go-api/internal/shared/runloop"
	"context"

	"golang.org/x/sync/errgroup"
)

type Group struct {
	event string
	g     errgroup.Group
}

func New(event string, limit int) *Group {
	g := &Group{event: event}
	g.g.SetLimit(limit)
	return g
}

func (g *Group) Go(ctx context.Context, fn func()) {
	g.g.Go(func() error { return runloop.Try(ctx, g.event, fn) })
}

func (g *Group) Wait() error {
	return g.g.Wait()
}
