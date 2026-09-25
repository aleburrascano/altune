package core_test

import (
	"altune/overseer/internal/core"
	"testing"
)

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
