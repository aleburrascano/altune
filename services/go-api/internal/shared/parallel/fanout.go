package parallel

import (
	"altune/go-api/internal/shared/background"
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
)

type Limited struct {
	event string
	g     errgroup.Group
}

func New(event string, limit int) *Limited {
	g := &Limited{event: event}
	g.g.SetLimit(limit)
	return g
}

func (g *Limited) Go(ctx context.Context, fn func(), attrs ...any) {
	g.g.Go(func() error { return background.SafeRun(ctx, g.event, fn, attrs...) })
}

func (g *Limited) Wait() error {
	if err := g.g.Wait(); err != nil {
		return fmt.Errorf("wait: %w", err)
	}
	return nil
}
