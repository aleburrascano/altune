package ytdlp

import (
	"context"
	"fmt"
	"testing"

	"altune/go-api/internal/acquisition/ports"
)

func findRequest() ports.FindRequest {
	return ports.FindRequest{
		Title:  "Blinding Lights",
		Artist: "The Weeknd",
		Album:  "After Hours",
		ISRC:   "USUG11904206",
	}
}

func candidatesPerSpec(spec string, n int) []ports.AudioCandidate {
	out := make([]ports.AudioCandidate, 0, n)
	for i := range n {
		out = append(out, ports.AudioCandidate{
			Title: spec,
			URL:   fmt.Sprintf("https://example.test/%s/%d", spec, i),
		})
	}
	return out
}

func TestSource_Find_StopsSearchingOnceEnoughCandidatesAreFound(t *testing.T) {
	var specs []string
	src := NewSource(withRunner(func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		specs = append(specs, spec)
		return candidatesPerSpec(spec, 5), nil
	}))

	got, err := src.Find(context.Background(), findRequest())
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if len(specs) > len(searchEngines) {
		t.Fatalf("searches = %d %v, want no more than the first query's %d once %d candidates are in",
			len(specs), specs, len(searchEngines), ports.EnoughCandidates)
	}
	if len(got) < ports.EnoughCandidates {
		t.Fatalf("merged candidates = %d, want at least %d", len(got), ports.EnoughCandidates)
	}
}

func TestSource_Find_RunsEveryQueryWhileCandidatesStayScarce(t *testing.T) {
	var specs []string
	src := NewSource(withRunner(func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		specs = append(specs, spec)
		return candidatesPerSpec(spec, 1), nil
	}))

	got, err := src.Find(context.Background(), findRequest())
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	wantSearches := len(ports.SearchQueries(findRequest())) * len(searchEngines)
	if len(specs) != wantSearches {
		t.Fatalf("searches = %d %v, want all %d when no query is fruitful", len(specs), specs, wantSearches)
	}
	if len(got) != wantSearches {
		t.Fatalf("merged candidates = %d, want %d", len(got), wantSearches)
	}
}
