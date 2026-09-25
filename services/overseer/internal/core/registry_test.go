package core_test

import (
	"altune/overseer/internal/core"
	"context"
	"testing"
)

// fakeBucket is a minimal Bucket used to exercise the registry without importing
// any concrete bucket package — proving the core needs none.
type fakeBucket struct{ id string }

func (f fakeBucket) Meta() core.Meta                                { return core.Meta{ID: f.id, Title: f.id} }
func (f fakeBucket) Collect(context.Context) ([]core.Signal, error) { return nil, nil }
func (f fakeBucket) Store([]core.Signal)                            {}
func (f fakeBucket) Snapshot() core.Snapshot                        { return core.Snapshot{ID: f.id} }

// Spine invariant: additive buckets. Registering a second bucket touches only a
// fresh registry call and never any existing bucket — the registry treats every
// bucket the same and keeps both.
func TestRegistryIsAdditive(t *testing.T) {
	r := core.NewRegistry()
	r.Register(fakeBucket{id: "first"})
	r.Register(fakeBucket{id: "second"})

	got := r.Buckets()
	if len(got) != 2 {
		t.Fatalf("Buckets len = %d, want 2", len(got))
	}
	// Deterministic, ID-sorted order regardless of registration order.
	if got[0].Meta().ID != "first" || got[1].Meta().ID != "second" {
		t.Fatalf("order = %s,%s, want first,second", got[0].Meta().ID, got[1].Meta().ID)
	}
}

func TestRegistryRejectsDuplicateID(t *testing.T) {
	r := core.NewRegistry()
	r.Register(fakeBucket{id: "dup"})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	r.Register(fakeBucket{id: "dup"})
}

func TestRegistryRejectsEmptyID(t *testing.T) {
	r := core.NewRegistry()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on empty ID")
		}
	}()
	r.Register(fakeBucket{id: ""})
}

func TestRegistryGetReturnsTheRegisteredBucket(t *testing.T) {
	r := core.NewRegistry()
	r.Register(fakeBucket{id: "reliability"})

	got, ok := r.Get("reliability")
	if !ok {
		t.Fatal("Get(reliability) found nothing, want the registered bucket")
	}
	if got.Meta().ID != "reliability" {
		t.Fatalf("Get(reliability) returned %q", got.Meta().ID)
	}
	if _, ok := r.Get("missing"); ok {
		t.Fatal("Get(missing) reported a bucket that was never registered")
	}
}
