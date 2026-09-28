package core

import (
	"context"
)

type Meta struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type Bucket interface {
	Meta() Meta
	Collect(ctx context.Context) ([]Signal, error)
	Store(signals []Signal)
	Snapshot() Snapshot
}

type Starter interface {
	Start(ctx context.Context)
}

type Waiter interface {
	Wait()
}

type Restorable interface {
	Rings() map[string]*RingStore
}

type KeySeries interface {
	KeySeries() string
}
