package app

import (
	"altune/overseer/internal/core"
	"context"
	"testing"
)

// panicBucket panics in Collect and Store to prove the collect loop contains a
// misbehaving bucket instead of crashing the process — the degrade-don't-crash
// invariant on the collect side, matching shell.safeRender on the render side.
type panicBucket struct{ where string }

func (panicBucket) Meta() core.Meta { return core.Meta{ID: "boom", Title: "Boom"} }
func (p panicBucket) Collect(context.Context) ([]core.Signal, error) {
	if p.where == "collect" {
		panic("collect blew up")
	}
	return []core.Signal{{Text: "ok"}}, nil
}

func (p panicBucket) Store([]core.Signal) {
	if p.where == "store" {
		panic("store blew up")
	}
}
func (panicBucket) Render() core.Panel { return core.Panel{} }

// TestSafeCollectContainsPanic: a bucket panicking in Collect yields an error,
// not a process-killing panic.
func TestSafeCollectContainsPanic(t *testing.T) {
	signals, err := safeCollect(context.Background(), panicBucket{where: "collect"})
	if err == nil {
		t.Fatal("safeCollect returned nil error for a panicking bucket; the panic was not contained")
	}
	if signals != nil {
		t.Errorf("safeCollect returned signals %v after a panic; want nil", signals)
	}
}

// TestSafeStoreContainsPanic: a bucket panicking in Store must not escape.
func TestSafeStoreContainsPanic(t *testing.T) {
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("safeStore let a bucket panic escape: %v", rec)
		}
	}()
	safeStore(context.Background(), panicBucket{where: "store"}, []core.Signal{{Text: "x"}})
}
