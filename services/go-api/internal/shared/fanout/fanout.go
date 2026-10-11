package fanout

import "golang.org/x/sync/errgroup"

type Group struct {
	g errgroup.Group
}

func New(limit int) *Group {
	g := &Group{}
	g.g.SetLimit(limit)
	return g
}

func (g *Group) Go(fn func()) {
	g.g.Go(func() error {
		fn()
		return nil
	})
}

func (g *Group) Wait() {
	_ = g.g.Wait()
}
