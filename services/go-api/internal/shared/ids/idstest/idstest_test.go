package idstest_test

import (
	"altune/go-api/internal/shared/ids/idstest"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestSequence_CountsUpFromOne(t *testing.T) {
	src := idstest.Sequence()

	got := []string{src.NewString(), src.NewString(), src.NewString()}

	want := []string{
		"00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002",
		"00000000-0000-0000-0000-000000000003",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ids = %v, want %v", got, want)
		}
	}
}

func TestSequence_EachSourceStartsOver(t *testing.T) {
	first, second := idstest.Sequence(), idstest.Sequence()

	first.NewUserId()

	if got := second.NewUserId().String(); got != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("a new source's first id = %s, want the first of the sequence", got)
	}
}

func TestSequence_IsSafeAndUniqueAcrossGoroutines(t *testing.T) {
	src := idstest.Sequence()
	var mu sync.Mutex
	var wg sync.WaitGroup
	seen := map[uuid.UUID]bool{}

	for range 8 {
		wg.Go(func() {
			for range 500 {
				id := src()
				mu.Lock()
				if seen[id] {
					t.Errorf("id %v was issued twice", id)
				}
				seen[id] = true
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	if len(seen) != 4000 {
		t.Fatalf("issued %d distinct ids, want 4000", len(seen))
	}
}
