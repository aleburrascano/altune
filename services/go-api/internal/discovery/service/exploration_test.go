package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestMaybeExplore_DisabledIsInert(t *testing.T) {
	svc := NewService(nil, NewCircuitBreaker()) // explorationRate 0
	in := []domain.SearchResult{{Title: "a"}, {Title: "b"}, {Title: "c"}}
	out, explored := svc.maybeExplore(in)
	if explored {
		t.Error("exploration must be off when rate is 0")
	}
	if &out[0] != &in[0] {
		t.Error("inert path must return the same slice, not a copy")
	}
}

func TestMaybeExplore_AlwaysExploresClonesAndKeepsMembers(t *testing.T) {
	svc := NewService(nil, NewCircuitBreaker(), WithExploration(1.0)) // always explore
	in := []domain.SearchResult{{Title: "a"}, {Title: "b"}, {Title: "c"}}
	out, explored := svc.maybeExplore(in)

	if !explored {
		t.Fatal("rate 1.0 must always explore")
	}
	// Cache-safety: the input slice must be untouched (a shared cached list).
	if in[0].Title != "a" || in[1].Title != "b" || in[2].Title != "c" {
		t.Error("maybeExplore must not mutate the input (cache) slice")
	}
	// Same membership, just possibly reordered.
	seen := map[string]bool{}
	for _, r := range out {
		seen[r.Title] = true
	}
	if len(out) != 3 || !seen["a"] || !seen["b"] || !seen["c"] {
		t.Errorf("exploration must preserve membership, got %v", out)
	}
}
